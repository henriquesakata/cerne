package sourceurl

import "testing"

// F-0503 crit. 5: a name that contains "tok", the per-recipient tracking
// parameters of mail campaigns, a JWT-shaped value under any name, and the
// webhook paths of Discord and Telegram mark the URL.
func TestHasSecretRound2(t *testing.T) {
	const jwt = "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjMifQ.c2lnbmF0dXJl"
	for _, u := range []string{
		"https://docs.publicsite.org/p?tok=abc",
		"https://docs.publicsite.org/p?x_tok=abc",
		"https://docs.publicsite.org/p?mc_eid=1a2b3c",
		"https://docs.publicsite.org/p?_hsenc=p2ANqtz",
		"https://docs.publicsite.org/p?lang=en&_hsmi=12345",
		"https://docs.publicsite.org/p#_hsenc=abc",
		"https://docs.publicsite.org/p?next=" + jwt,
		"https://docs.publicsite.org/p#state=" + jwt,
		"https://docs.publicsite.org/p?x=" + jwt + "&y=1",
		"https://discord.com/api/webhooks/123456789/AbCdEf",
		"https://api.telegram.org/bot123456:ABC-DEF/getMe",
	} {
		if !HasSecret(u) {
			t.Errorf("HasSecret(%q) = false, want true", u)
		}
	}
	for _, u := range []string{
		"https://docs.publicsite.org/p?next=eyJhbGciOiJIUzI1NiJ9.only-two",
		"https://docs.publicsite.org/p?x=notajwt.a.b",
		"https://docs.publicsite.org/api/webhook-guide",
		"https://docs.publicsite.org/robots.txt",
		"https://docs.publicsite.org/bot/intro",
	} {
		if HasSecret(u) {
			t.Errorf("HasSecret(%q) = true, want false", u)
		}
	}
}
