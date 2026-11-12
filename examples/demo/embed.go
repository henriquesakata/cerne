// Package demo embeds the documents of the taskflow example project, so
// `cerne demo` builds it from the binary alone. seed.sh reads the same files.
package demo

import "embed"

// Content holds the documents under "content/...".
//
//go:embed content
var Content embed.FS
