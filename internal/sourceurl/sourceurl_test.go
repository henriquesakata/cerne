package sourceurl

import (
	"strings"
	"testing"
)

// F-0503 crit. 4: localhost, .local, private and loopback IPs and any
// configured ignore substring are internal; a public host is not.
func TestInternal(t *testing.T) {
	ignore := []string{"github.com/acme/private-repo"}
	for _, tc := range []struct {
		url  string
		want bool
	}{
		{"http://localhost:8080/x", true},
		{"https://wiki.local/page", true},
		{"http://10.1.2.3/x", true},
		{"http://172.16.0.9/x", true},
		{"http://192.168.0.1/x", true},
		{"http://127.0.0.1/x", true},
		{"http://[::1]/x", true},
		{"http://[fd00::1]/x", true},
		{"https://github.com/acme/private-repo/blob/main/x.go", true},
		{"https://github.com/acme/public-repo", false},
		{"https://docs.publicsite.org/guide", false},
		{"http://172.32.0.1/x", false},
		{"::not a url", false},
	} {
		if got := Internal(tc.url, ignore); got != tc.want {
			t.Errorf("Internal(%q) = %v, want %v", tc.url, got, tc.want)
		}
	}
}

// F-0503 crit. 5: a user or password in the URL, or a query (or fragment)
// parameter named like a secret, marks the URL; a plain one does not.
func TestHasSecret(t *testing.T) {
	for _, name := range []string{"token", "key", "secret", "sig", "signature", "auth", "password", "access_token"} {
		for _, u := range []string{
			"https://docs.publicsite.org/page?" + name + "=abc",
			"https://docs.publicsite.org/page?a=1&" + strings.ToUpper(name) + "=abc",
			"https://docs.publicsite.org/page#" + name + "=abc",
		} {
			if !HasSecret(u) {
				t.Errorf("HasSecret(%q) = false, want true", u)
			}
		}
	}
	for _, u := range []string{
		"https://user@docs.publicsite.org/page",
		"https://user:pass@docs.publicsite.org/page",
		"https://s3.example.org/f?X-Amz-Signature=abc",
		"https://api.example.org/f?api_key=abc",
		"https://api.example.org/f?client_secret=abc",
		"%zz://bad",
	} {
		if !HasSecret(u) {
			t.Errorf("HasSecret(%q) = false, want true", u)
		}
	}
	// F-0503 crit. 5: a secret word anywhere in the name counts, in the query
	// or the fragment.
	for _, name := range []string{
		"apikey", "sessionid", "jwt", "pwd", "passwd", "X-Amz-Credential", "bearer", "code",
		"client_secret", "accesstoken", "oauth_verifier", "PHPSESSID", "id_token", "auth%5Fcode",
	} {
		for _, u := range []string{
			"https://docs.publicsite.org/page?lang=en&" + name + "=abc",
			"https://docs.publicsite.org/page#" + name + "=abc",
		} {
			if !HasSecret(u) {
				t.Errorf("HasSecret(%q) = false, want true", u)
			}
		}
	}
	// F-0503 crit. 5: fail closed on a query the parser would drop or reject.
	for _, u := range []string{
		"https://docs.publicsite.org/page?a=1;token=x",
		"https://docs.publicsite.org/page?a=1;b=2",
		"https://docs.publicsite.org/page?a=%zz",
		"https://docs.publicsite.org/page?%zz=1",
		"https://docs.publicsite.org/page#a=1;b=2",
	} {
		if !HasSecret(u) {
			t.Errorf("HasSecret(%q) = false, want true (fail closed)", u)
		}
	}
	// Accepted over-blocking: a harmless name with a secret word inside.
	for _, u := range []string{"https://docs.publicsite.org/p?barcode=1", "https://docs.publicsite.org/p?unicode=1", "https://docs.publicsite.org/p?keyword=go"} {
		if !HasSecret(u) {
			t.Errorf("HasSecret(%q) = false, want true (substring match over-blocks)", u)
		}
	}
	for _, u := range []string{
		"https://docs.publicsite.org/page",
		"https://docs.publicsite.org/page?q=go&lang=en&page=2",
		"https://docs.publicsite.org/page#section-2",
		"https://docs.publicsite.org/tokens/auth-guide",
	} {
		if HasSecret(u) {
			t.Errorf("HasSecret(%q) = true, want false", u)
		}
	}
}
