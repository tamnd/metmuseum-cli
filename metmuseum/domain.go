package metmuseum

import (
	"context"
	"fmt"
	"strconv"

	"github.com/tamnd/any-cli/kit"
	"github.com/tamnd/any-cli/kit/errs"
)

func init() { kit.Register(Domain{}) }

// Domain is the metmuseum driver.
type Domain struct{}

// Info describes the scheme, hostnames, and binary identity.
func (Domain) Info() kit.DomainInfo {
	return kit.DomainInfo{
		Scheme: "metmuseum",
		Hosts:  []string{Host},
		Identity: kit.Identity{
			Binary: "metmuseum",
			Short:  "A command line for the Metropolitan Museum of Art.",
			Long: `A command line for the Metropolitan Museum of Art public Collection API.

metmuseum reads artwork objects, departments, and search results from
collectionapi.metmuseum.org over HTTPS, shapes them into clean records,
and prints output that pipes into the rest of your tools. No API key required.`,
			Site: Host,
			Repo: "https://github.com/tamnd/metmuseum-cli",
		},
	}
}

// Register installs the client factory and every operation onto app.
func (Domain) Register(app *kit.App) {
	app.SetClient(newClient)

	kit.Handle(app, kit.OpMeta{Name: "search", Group: "read", List: true,
		Summary: "Search objects by keyword (--has-images, --is-public-domain, --department, --limit)",
		Args:    []kit.Arg{{Name: "query", Help: "search query"}}}, searchObjects)

	kit.Handle(app, kit.OpMeta{Name: "object", Group: "read", Single: true,
		Summary: "Get a single object by ID",
		Args:    []kit.Arg{{Name: "id", Help: "object numeric ID"}}}, getObject)

	kit.Handle(app, kit.OpMeta{Name: "departments", Group: "read", List: true,
		Summary: "List all museum departments"}, listDepartments)
}

// newClient builds the Client from kit config.
func newClient(_ context.Context, cfg kit.Config) (any, error) {
	c := DefaultConfig()
	if cfg.UserAgent != "" {
		c.UserAgent = cfg.UserAgent
	}
	if cfg.Rate > 0 {
		c.Rate = cfg.Rate
	}
	if cfg.Retries > 0 {
		c.Retries = cfg.Retries
	}
	if cfg.Timeout > 0 {
		c.Timeout = cfg.Timeout
	}
	return NewClient(c), nil
}

// --- input structs ---

type searchInput struct {
	Query        string  `kit:"arg" help:"search query"`
	HasImages    bool    `kit:"flag" help:"filter to objects with images"`
	PublicDomain bool    `kit:"flag" help:"filter to public domain objects"`
	Department   int     `kit:"flag" help:"department ID filter"`
	Limit        int     `kit:"flag,inherit" help:"max results"`
	Client       *Client `kit:"inject"`
}

type objectInput struct {
	ID     string  `kit:"arg" help:"object numeric ID"`
	Client *Client `kit:"inject"`
}

type departmentsInput struct {
	Client *Client `kit:"inject"`
}

// --- handlers ---

func searchObjects(ctx context.Context, in searchInput, emit func(*Object) error) error {
	items, err := in.Client.SearchObjects(ctx, in.Query, in.HasImages, in.PublicDomain, in.Department, in.Limit)
	if err != nil {
		return err
	}
	for i := range items {
		if err := emit(&items[i]); err != nil {
			return err
		}
	}
	return nil
}

func getObject(ctx context.Context, in objectInput, emit func(*Object) error) error {
	id, err := strconv.Atoi(in.ID)
	if err != nil {
		return errs.Usage("object id must be a number, got %q", in.ID)
	}
	item, err := in.Client.GetObject(ctx, id)
	if err != nil {
		return err
	}
	return emit(item)
}

func listDepartments(ctx context.Context, in departmentsInput, emit func(*Department) error) error {
	items, err := in.Client.ListDepartments(ctx)
	if err != nil {
		return err
	}
	for i := range items {
		if err := emit(&items[i]); err != nil {
			return err
		}
	}
	return nil
}

// Classify turns a URL or ID into (type, id).
func (Domain) Classify(input string) (string, string, error) {
	return "object", input, nil
}

// Locate returns the live https URL for a (type, id).
func (Domain) Locate(t, id string) (string, error) {
	switch t {
	case "object":
		return fmt.Sprintf("https://www.metmuseum.org/art/collection/search/%s", id), nil
	default:
		return "", errs.Usage("metmuseum has no resource type %q", t)
	}
}
