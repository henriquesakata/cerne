package sourceurl

import "testing"

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
