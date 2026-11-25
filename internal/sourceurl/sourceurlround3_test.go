package sourceurl

import "testing"

// F-0503 crit. 5: per-recipient mail ids (oly_enc_id, vero_id, ck_subscriber_id,
// email) and a path segment shaped like a JWT mark the URL.
func TestHasSecretRound3(t *testing.T) {
	const jwt = "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjMifQ.c2lnbmF0dXJl"
	for _, u := range []string{
		"https://docs.publicsite.org/p?oly_enc_id=123",
		"https://docs.publicsite.org/p?vero_id=123",
		"https://docs.publicsite.org/p?ck_subscriber_id=123",
		"https://docs.publicsite.org/p?email=a%40b.org",
		"https://docs.publicsite.org/p#EMAIL=a",
		"https://docs.publicsite.org/invite/" + jwt,
		"https://docs.publicsite.org/a/" + jwt + "/b",
	} {
		if !HasSecret(u) {
			t.Errorf("HasSecret(%q) = false, want true", u)
		}
	}
	for _, u := range []string{
		"https://docs.publicsite.org/emails/guide",
		"https://docs.publicsite.org/eyJhbGciOiJIUzI1NiJ9.only-two",
		"https://docs.publicsite.org/v1.2.3",
	} {
		if HasSecret(u) {
			t.Errorf("HasSecret(%q) = true, want false", u)
		}
	}
}
