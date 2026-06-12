// Package ui embeds the built front-end (ui/dist) into the cerne binary.
// dist/ is committed so `go build` yields the full binary from a clean
// clone; rebuild it with `cd ui && npm install && npm run build`.
package ui

import "embed"

// Dist holds the built front-end under "dist/...".
//
//go:embed all:dist
var Dist embed.FS
