// Package metmuseum is the library behind the metmuseum command line:
// the HTTP client, request shaping, and typed data models for the Metropolitan
// Museum of Art public Collection API
// (https://collectionapi.metmuseum.org/public/collection/v1).
//
// No API key is required. The Client paces requests, sets a real User-Agent,
// and retries transient failures (429 and 5xx).
package metmuseum

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// DefaultUserAgent identifies the client to the Met Museum API.
const DefaultUserAgent = "metmuseum-cli/0.1.0 (github.com/tamnd/metmuseum-cli)"

// Host is the API hostname.
const Host = "collectionapi.metmuseum.org"

// BaseURL is the root every API request is built from.
const BaseURL = "https://" + Host + "/public/collection/v1"

// Object holds the public data about a single museum object.
type Object struct {
	ID                int    `json:"id"`
	Title             string `json:"title"`
	Artist            string `json:"artist"`
	Nationality       string `json:"nationality,omitempty"`
	Date              string `json:"date,omitempty"`
	Medium            string `json:"medium,omitempty"`
	Dimensions        string `json:"dimensions,omitempty"`
	Department        string `json:"department,omitempty"`
	PublicDomain      bool   `json:"public_domain"`
	PrimaryImageSmall string `json:"primary_image_small,omitempty"`
	URL               string `json:"url,omitempty"`
}

// Department holds museum department data.
type Department struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// SearchResult holds raw search results (IDs only).
type SearchResult struct {
	Total     int   `json:"total"`
	ObjectIDs []int `json:"object_ids"`
}

// --- wire types ---

type wireObject struct {
	ObjectID          int    `json:"objectID"`
	Title             string `json:"title"`
	ArtistDisplayName string `json:"artistDisplayName"`
	ArtistNationality string `json:"artistNationality"`
	ObjectDate        string `json:"objectDate"`
	Medium            string `json:"medium"`
	Dimensions        string `json:"dimensions"`
	Department        string `json:"department"`
	IsPublicDomain    bool   `json:"isPublicDomain"`
	PrimaryImageSmall string `json:"primaryImageSmall"`
	ObjectURL         string `json:"objectURL"`
}

type wireDepartment struct {
	DepartmentID int    `json:"departmentId"`
	DisplayName  string `json:"displayName"`
}

type wireSearchResult struct {
	Total     int   `json:"total"`
	ObjectIDs []int `json:"objectIDs"`
}

type wireDepartmentsResp struct {
	Departments []wireDepartment `json:"departments"`
}

// Config holds all tunable parameters for the Client.
type Config struct {
	BaseURL   string
	UserAgent string
	Rate      time.Duration
	Timeout   time.Duration
	Retries   int
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		BaseURL:   BaseURL,
		UserAgent: DefaultUserAgent,
		Rate:      200 * time.Millisecond,
		Timeout:   30 * time.Second,
		Retries:   3,
	}
}

// Client talks to the Metropolitan Museum of Art API.
type Client struct {
	cfg  Config
	http *http.Client
	mu   sync.Mutex
	last time.Time
}

// NewClient returns a Client with the given configuration.
func NewClient(cfg Config) *Client {
	return &Client{
		cfg:  cfg,
		http: &http.Client{Timeout: cfg.Timeout},
	}
}

// SearchObjects searches objects and fetches full details for the top results.
// It calls /search first, then fans out to /objects/{id} for each ID up to limit.
func (c *Client) SearchObjects(ctx context.Context, q string, hasImages, publicDomain bool, deptID, limit int) ([]Object, error) {
	if limit <= 0 {
		limit = 20
	}

	params := url.Values{}
	params.Set("q", q)
	if hasImages {
		params.Set("hasImages", "true")
	}
	if publicDomain {
		params.Set("isPublicDomain", "true")
	}
	if deptID > 0 {
		params.Set("departmentId", fmt.Sprintf("%d", deptID))
	}

	rawURL := c.cfg.BaseURL + "/search?" + params.Encode()
	body, err := c.get(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	var sr wireSearchResult
	if err := json.Unmarshal(body, &sr); err != nil {
		return nil, fmt.Errorf("parse search: %w", err)
	}

	ids := sr.ObjectIDs
	if len(ids) > limit {
		ids = ids[:limit]
	}

	// Fan out to fetch object details with bounded concurrency.
	type result struct {
		idx int
		obj *Object
		err error
	}
	results := make([]result, len(ids))
	sem := make(chan struct{}, 5)
	var wg sync.WaitGroup

	for i, id := range ids {
		wg.Add(1)
		go func(idx, objID int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			obj, err := c.GetObject(ctx, objID)
			results[idx] = result{idx: idx, obj: obj, err: err}
		}(i, id)
	}
	wg.Wait()

	out := make([]Object, 0, len(ids))
	for _, r := range results {
		if r.err != nil {
			continue // skip individual errors; return what we have
		}
		if r.obj != nil {
			out = append(out, *r.obj)
		}
	}
	return out, nil
}

// GetObject fetches a single museum object by numeric ID.
func (c *Client) GetObject(ctx context.Context, id int) (*Object, error) {
	rawURL := fmt.Sprintf("%s/objects/%d", c.cfg.BaseURL, id)
	body, err := c.get(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	var w wireObject
	if err := json.Unmarshal(body, &w); err != nil {
		return nil, fmt.Errorf("parse object %d: %w", id, err)
	}
	o := flattenObject(w)
	return &o, nil
}

// ListDepartments lists all museum departments.
func (c *Client) ListDepartments(ctx context.Context) ([]Department, error) {
	rawURL := c.cfg.BaseURL + "/departments"
	body, err := c.get(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	var resp wireDepartmentsResp
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse departments: %w", err)
	}
	return flattenDepartments(resp.Departments), nil
}

// get fetches a URL and returns the body, pacing and retrying as configured.
func (c *Client) get(ctx context.Context, rawURL string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= c.cfg.Retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff(attempt)):
			}
		}
		body, retry, err := c.do(ctx, rawURL)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if !retry {
			return nil, err
		}
	}
	return nil, fmt.Errorf("get %s: %w", rawURL, lastErr)
}

func (c *Client) do(ctx context.Context, rawURL string) (body []byte, retry bool, err error) {
	c.pace()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("User-Agent", c.cfg.UserAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, true, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return nil, true, fmt.Errorf("http %d", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("http %d", resp.StatusCode)
	}

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, true, err
	}
	return b, false, nil
}

func (c *Client) pace() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cfg.Rate <= 0 {
		return
	}
	if wait := c.cfg.Rate - time.Since(c.last); wait > 0 {
		time.Sleep(wait)
	}
	c.last = time.Now()
}

func backoff(attempt int) time.Duration {
	d := time.Duration(attempt) * 500 * time.Millisecond
	if d > 5*time.Second {
		d = 5 * time.Second
	}
	return d
}

// --- flatten helpers ---

func flattenObject(w wireObject) Object {
	return Object{
		ID:                w.ObjectID,
		Title:             w.Title,
		Artist:            w.ArtistDisplayName,
		Nationality:       w.ArtistNationality,
		Date:              w.ObjectDate,
		Medium:            w.Medium,
		Dimensions:        w.Dimensions,
		Department:        w.Department,
		PublicDomain:      w.IsPublicDomain,
		PrimaryImageSmall: w.PrimaryImageSmall,
		URL:               w.ObjectURL,
	}
}

func flattenDepartments(ws []wireDepartment) []Department {
	out := make([]Department, len(ws))
	for i, w := range ws {
		out[i] = Department{ID: w.DepartmentID, Name: w.DisplayName}
	}
	return out
}
