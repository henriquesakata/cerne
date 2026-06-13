package cli

import (
	"reflect"
	"testing"
)

func TestBrowserCommand(t *testing.T) {
	const url = "http://localhost:7717"
	tests := []struct {
		goos     string
		wantName string
		wantArgs []string
	}{
		{"windows", "rundll32", []string{"url.dll,FileProtocolHandler", url}},
		{"darwin", "open", []string{url}},
		{"linux", "xdg-open", []string{url}},
		{"freebsd", "xdg-open", []string{url}},
	}
	for _, tt := range tests {
		t.Run(tt.goos, func(t *testing.T) {
			name, args := browserCommand(tt.goos, url)
			if name != tt.wantName {
				t.Fatalf("name = %q, want %q", name, tt.wantName)
			}
			if !reflect.DeepEqual(args, tt.wantArgs) {
				t.Fatalf("args = %v, want %v", args, tt.wantArgs)
			}
		})
	}
}
