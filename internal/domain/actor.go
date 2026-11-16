package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// MachinePseudonym is "m-" plus the first 8 hex of the SHA-256 of the
// lowercased "<user>@<host>" (F-0442): stable per user and machine, no login
// or host in the clear.
func MachinePseudonym(userAtHost string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(userAtHost)))
	return "m-" + hex.EncodeToString(sum[:])[:8]
}

// ActorLabel keeps an "agent:" prefix and swaps an e-mail-shaped remainder
// ("<user>@<host>") for its MachinePseudonym; any other value (a name, "agent",
// "human", "agent:session-<8hex>") is returned unchanged.
func ActorLabel(raw string) string {
	prefix := ""
	rest := raw
	if strings.HasPrefix(raw, "agent:") {
		prefix, rest = "agent:", strings.TrimPrefix(raw, "agent:")
	}
	if strings.Contains(rest, "@") {
		return prefix + MachinePseudonym(rest)
	}
	return raw
}
