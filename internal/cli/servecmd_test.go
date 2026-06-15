package cli

import (
	"strings"
	"testing"
)

// TestServeFailsFastWithoutSecret: `cerne serve` refuses to start when the S2S
// shared secret is not configured, with an actionable message.
func TestServeFailsFastWithoutSecret(t *testing.T) {
	t.Setenv("CERNE_SERVE_SECRET", "")
	out, err := execCerne(t, "serve")
	if err == nil {
		t.Fatalf("serve without CERNE_SERVE_SECRET should error\noutput: %s", out)
	}
	if !strings.Contains(err.Error(), "CERNE_SERVE_SECRET") {
		t.Fatalf("error %q should mention CERNE_SERVE_SECRET", err)
	}
}
