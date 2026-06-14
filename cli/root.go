// Package cli assembles the metmuseum command tree from the metmuseum
// domain on top of the any-cli/kit framework.
package cli

import (
	"github.com/tamnd/any-cli/kit"
	"github.com/tamnd/metmuseum-cli/metmuseum"
)

// Build metadata, set via -ldflags at release time.
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

// NewApp assembles the kit application from the metmuseum domain. The
// domain's Register installs the client factory and every operation, so the
// binary and a host (ant, which blank-imports the package) share one source of
// truth. kit.Run turns the App into the CLI, plus the serve and mcp surfaces and
// the typed-error-to-exit-code mapping.
//
// To add a command, declare it in metmuseum/domain.go with kit.Handle and it
// appears here automatically. Reach for app.AddCommand only for a verb that does
// not fit the emit-records shape, the way version does below.
func NewApp() *kit.App {
	id := metmuseum.Domain{}.Info().Identity
	id.Version = Version

	app := kit.New(id)
	(metmuseum.Domain{}).Register(app)
	app.AddCommand(newVersionCmd())
	return app
}
