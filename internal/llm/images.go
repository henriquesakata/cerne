package llm

import (
	"context"
	"path/filepath"
	"slices"
)

// imagesKey carries the image files of a chat turn (F-0520) from the caller to
// the provider through the context, like envKey: the Provider interface and
// every text-only call stay as they are.
type imagesKey struct{}

// WithImages returns ctx carrying the absolute paths of the images attached to
// the conversation. A provider whose mode SupportsImages hands them to its CLI.
func WithImages(ctx context.Context, paths []string) context.Context {
	if len(paths) == 0 {
		return ctx
	}
	return context.WithValue(ctx, imagesKey{}, slices.Clone(paths))
}

// ImagesFrom is the image paths WithImages put in ctx, nil when none.
func ImagesFrom(ctx context.Context) []string {
	paths, _ := ctx.Value(imagesKey{}).([]string)
	return paths
}

// imageDirs is the distinct directories of the images, in first-seen order.
func imageDirs(paths []string) []string {
	var dirs []string
	for _, p := range paths {
		if d := filepath.Dir(p); !slices.Contains(dirs, d) {
			dirs = append(dirs, d)
		}
	}
	return dirs
}

// SupportsImages reports whether a turn in mode can be given images: the CLIs
// with an image input of their own. It is the one list the server and the UI's
// capability answer read.
func SupportsImages(mode string) bool {
	switch mode {
	case ModeClaudeCLI, ModeCodexCLI, ModeCopilotCLI:
		return true
	}
	return false
}
