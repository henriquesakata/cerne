package llm

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
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
// with an image input of their own and the API providers (F-0522), which take
// the image in the call. It is the one list the server and the UI's capability
// answer read. gemini-cli was never probed, and cursor-cli reads an image only
// without its Read deny, which also opens the rest of the disk.
func SupportsImages(mode string) bool {
	switch mode {
	case ModeClaudeCLI, ModeCodexCLI, ModeCopilotCLI, ModeByoKey:
		return true
	}
	return false
}

// apiImageTypes are the image types both APIs take; anything else would reach
// the provider as a media type it refuses (or a broken data URL).
var apiImageTypes = []string{"image/png", "image/jpeg", "image/webp", "image/gif"}

// imageData is one image of the turn, ready for an API body.
type imageData struct {
	mediaType string // read from the bytes, never from the file name
	base64    string
}

// loadImages reads the images WithImages put in ctx (F-0522). A file that
// cannot be read fails the call, naming it: the turn is never answered as if
// the image were not there.
func loadImages(ctx context.Context) ([]imageData, error) {
	var out []imageData
	for _, path := range ImagesFrom(ctx) {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("attached image %s: %w", path, err)
		}
		mediaType := http.DetectContentType(data)
		if !slices.Contains(apiImageTypes, mediaType) {
			return nil, fmt.Errorf("attached image %s: %s is not a PNG, JPEG, WebP or GIF image", path, mediaType)
		}
		out = append(out, imageData{mediaType: mediaType, base64: base64.StdEncoding.EncodeToString(data)})
	}
	return out, nil
}
