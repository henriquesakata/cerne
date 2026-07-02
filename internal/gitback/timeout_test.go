package gitback

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
)

// hangingRemote starts a local TCP listener that accepts connections and never
// responds — the git protocol client blocks waiting for the server's ref
// advertisement, reproducing the observed transport hang. Returns a git:// URL
// pointing at it.
func hangingRemote(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return // listener closed by cleanup; accumulated defers close conns
			}
			defer conn.Close()
		}
	}()
	return fmt.Sprintf("git://%s/repo.git", ln.Addr().String())
}

// setOriginURL repoints workdir's origin remote at url.
func setOriginURL(t *testing.T, workdir, url string) {
	t.Helper()
	repo, err := git.PlainOpen(workdir)
	if err != nil {
		t.Fatalf("PlainOpen: %v", err)
	}
	cfg, err := repo.Config()
	if err != nil {
		t.Fatalf("Config: %v", err)
	}
	cfg.Remotes[remoteName].URLs = []string{url}
	if err := repo.SetConfig(cfg); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}
}

// withNetTimeout swaps the package network deadline for the test.
func withNetTimeout(t *testing.T, d time.Duration) {
	t.Helper()
	old := netTimeout
	netTimeout = d
	t.Cleanup(func() { netTimeout = old })
}

func TestPushTimesOutOnHangingRemote(t *testing.T) {
	_, clone1, _ := fixture(t)
	setOriginURL(t, clone1, hangingRemote(t))
	withNetTimeout(t, 200*time.Millisecond)

	start := time.Now()
	err := Push(clone1)
	elapsed := time.Since(start)
	if !errors.Is(err, ErrNetworkTimeout) {
		t.Fatalf("Push err = %v, want ErrNetworkTimeout", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Push err = %v, want wrapped context.DeadlineExceeded", err)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("Push returned after %v, want prompt return once the 200ms deadline expired", elapsed)
	}
}

func TestFetchTimesOutOnHangingRemote(t *testing.T) {
	_, clone1, _ := fixture(t)
	setOriginURL(t, clone1, hangingRemote(t))
	withNetTimeout(t, 200*time.Millisecond)

	start := time.Now()
	err := Fetch(clone1)
	elapsed := time.Since(start)
	if !errors.Is(err, ErrNetworkTimeout) {
		t.Fatalf("Fetch err = %v, want ErrNetworkTimeout", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Fetch err = %v, want wrapped context.DeadlineExceeded", err)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("Fetch returned after %v, want prompt return once the 200ms deadline expired", elapsed)
	}
}

func TestNetTimeoutFromEnv(t *testing.T) {
	cases := []struct {
		name string
		val  string
		want time.Duration
	}{
		{"unset", "", defaultNetTimeout},
		{"valid", "90s", 90 * time.Second},
		{"invalid", "bananas", defaultNetTimeout},
		{"nonpositive", "-5s", defaultNetTimeout},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CERNE_NET_TIMEOUT", tc.val)
			if got := netTimeoutFromEnv(); got != tc.want {
				t.Fatalf("netTimeoutFromEnv(%q) = %v, want %v", tc.val, got, tc.want)
			}
		})
	}
}
