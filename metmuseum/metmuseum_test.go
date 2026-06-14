package metmuseum_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tamnd/metmuseum-cli/metmuseum"
)

func newTestClient(ts *httptest.Server) *metmuseum.Client {
	cfg := metmuseum.DefaultConfig()
	cfg.BaseURL = ts.URL
	cfg.Rate = 0
	return metmuseum.NewClient(cfg)
}

// TestUserAgent checks that every request carries metmuseum-cli in User-Agent.
func TestUserAgent(t *testing.T) {
	var gotUA string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		resp := map[string]any{"departments": []any{}}
		b, _ := json.Marshal(resp)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(b)
	}))
	defer ts.Close()

	c := newTestClient(ts)
	_, _ = c.ListDepartments(context.Background())

	if !strings.Contains(gotUA, "metmuseum-cli") {
		t.Errorf("User-Agent = %q, want it to contain metmuseum-cli", gotUA)
	}
}

// TestGetObject checks that a single object is parsed with all fields.
func TestGetObject(t *testing.T) {
	fixture := map[string]any{
		"objectID":          436532,
		"title":             "Self-Portrait with a Straw Hat",
		"artistDisplayName": "Vincent van Gogh",
		"artistNationality": "Dutch",
		"objectDate":        "1887",
		"medium":            "Oil on canvas",
		"dimensions":        "16 × 12 5/8 in.",
		"department":        "European Paintings",
		"isPublicDomain":    true,
		"primaryImageSmall": "https://images.metmuseum.org/small.jpg",
		"objectURL":         "https://www.metmuseum.org/art/collection/search/436532",
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := json.Marshal(fixture)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(b)
	}))
	defer ts.Close()

	c := newTestClient(ts)
	obj, err := c.GetObject(context.Background(), 436532)
	if err != nil {
		t.Fatal(err)
	}
	if obj.ID != 436532 {
		t.Errorf("ID = %d, want 436532", obj.ID)
	}
	if obj.Title != "Self-Portrait with a Straw Hat" {
		t.Errorf("Title = %q", obj.Title)
	}
	if obj.Artist != "Vincent van Gogh" {
		t.Errorf("Artist = %q", obj.Artist)
	}
	if !obj.PublicDomain {
		t.Error("PublicDomain should be true")
	}
	if obj.PrimaryImageSmall == "" {
		t.Error("PrimaryImageSmall should not be empty")
	}
}

// TestSearchIDs checks that search response is parsed to objectIDs.
func TestSearchIDs(t *testing.T) {
	searchFixture := map[string]any{
		"total":     168,
		"objectIDs": []any{438003, 437133, 436532},
	}
	objectFixture := map[string]any{
		"objectID":          438003,
		"title":             "Water Lilies",
		"artistDisplayName": "Claude Monet",
		"isPublicDomain":    true,
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var resp any
		if strings.Contains(r.URL.Path, "search") {
			resp = searchFixture
		} else {
			resp = objectFixture
		}
		b, _ := json.Marshal(resp)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(b)
	}))
	defer ts.Close()

	c := newTestClient(ts)
	objs, err := c.SearchObjects(context.Background(), "monet", false, false, 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(objs) != 2 {
		t.Errorf("got %d objects, want 2", len(objs))
	}
}

// TestListDepartments checks that departments list is parsed correctly.
func TestListDepartments(t *testing.T) {
	fixture := map[string]any{
		"departments": []any{
			map[string]any{"departmentId": 1, "displayName": "American Decorative Arts"},
			map[string]any{"departmentId": 3, "displayName": "Ancient Near Eastern Art"},
		},
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := json.Marshal(fixture)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(b)
	}))
	defer ts.Close()

	c := newTestClient(ts)
	depts, err := c.ListDepartments(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(depts) != 2 {
		t.Fatalf("got %d departments, want 2", len(depts))
	}
	if depts[0].ID != 1 {
		t.Errorf("first dept ID = %d, want 1", depts[0].ID)
	}
	if depts[0].Name != "American Decorative Arts" {
		t.Errorf("first dept name = %q", depts[0].Name)
	}
}

// TestPublicDomainFlag checks that isPublicDomain=true is forwarded in the query.
func TestPublicDomainFlag(t *testing.T) {
	var gotQuery string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "search") {
			gotQuery = r.URL.RawQuery
			resp := map[string]any{"total": 0, "objectIDs": []any{}}
			b, _ := json.Marshal(resp)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(b)
		}
	}))
	defer ts.Close()

	c := newTestClient(ts)
	_, err := c.SearchObjects(context.Background(), "monet", true, true, 0, 5)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotQuery, "hasImages=true") {
		t.Errorf("query %q should contain hasImages=true", gotQuery)
	}
	if !strings.Contains(gotQuery, "isPublicDomain=true") {
		t.Errorf("query %q should contain isPublicDomain=true", gotQuery)
	}
}

// TestRetryOn503 checks that the client retries on 503 and succeeds eventually.
func TestRetryOn503(t *testing.T) {
	var hits int
	fixture := map[string]any{
		"departments": []any{
			map[string]any{"departmentId": 1, "displayName": "American Decorative Arts"},
		},
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		b, _ := json.Marshal(fixture)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(b)
	}))
	defer ts.Close()

	cfg := metmuseum.DefaultConfig()
	cfg.BaseURL = ts.URL
	cfg.Rate = 0
	cfg.Retries = 5
	c := metmuseum.NewClient(cfg)

	start := time.Now()
	_, err := c.ListDepartments(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if hits != 3 {
		t.Errorf("server saw %d hits, want 3", hits)
	}
	if time.Since(start) < 500*time.Millisecond {
		t.Error("retries did not back off")
	}
}
