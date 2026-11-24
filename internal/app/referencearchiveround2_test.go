package app

import (
	"net/url"
	"testing"
)

// F-0503 crit. 6: a path segment that contains login, signin, sign-in, sign_in
// or logon, starts with oauth, openid, saml or sso, or is auth, authorize or
// cas is a login page; over-blocking is accepted.
func TestLoginPageSegments(t *testing.T) {
	for _, tc := range []struct {
		u    string
		want bool
	}{
		{"https://x.org/wp-login.php", true},
		{"https://x.org/login.jsp", true},
		{"https://x.org/login.action?os_destination=%2Fhome", true},
		{"https://x.org/account/Logon.aspx", true},
		{"https://x.org/users/sign-in", true},
		{"https://x.org/signin/identifier", true},
		{"https://x.org/oauth2/authorize", true},
		{"https://x.org/openid-connect/auth", true},
		{"https://x.org/saml2/idp", true},
		{"https://x.org/sso/start", true},
		{"https://x.org/auth", true},
		{"https://x.org/authorize", true},
		{"https://x.org/cas/login", true},
		{"https://x.org/cas", true},
		{"https://accounts.google.com/anything", true},
		{"https://x.org/docs/guide", false},
		{"https://x.org/authors", false},
		{"https://x.org/blog/classes", false},
		{"https://x.org/caste/system", false},
	} {
		u, err := url.Parse(tc.u)
		if err != nil {
			t.Fatal(err)
		}
		if got := loginPage(u); got != tc.want {
			t.Errorf("loginPage(%s) = %v, want %v", tc.u, got, tc.want)
		}
	}
}
