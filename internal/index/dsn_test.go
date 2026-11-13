package index

import "testing"

func TestDSN(t *testing.T) {
	const q = "?_pragma=busy_timeout(5000)&_txlock=immediate"
	for _, c := range []struct{ in, want string }{
		{"/tmp/a b/index.db", "file:///tmp/a%20b/index.db" + q},
		{`C:\Users\me\index.db`, "file:///C:/Users/me/index.db" + q},
		{"C:/Users/me/index.db", "file:///C:/Users/me/index.db" + q},
	} {
		if got := dsn(c.in); got != c.want {
			t.Errorf("dsn(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
