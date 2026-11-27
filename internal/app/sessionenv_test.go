package app

import (
	"os"
	"testing"
)

// TestCurrentSession covers the resolver logic (F-0525 crit. 1 and crit. 5).
// F-0525 crit. 1
func TestCurrentSession(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  map[string]string
		want string
	}{
		{
			name: "CERNE_SESSION wins when both set",
			env: map[string]string{
				"CERNE_SESSION":          "aaaaaa00-1111-2222-3333-444444444444",
				"CURSOR_AGENT":           "1",
				"CURSOR_CONVERSATION_ID": "bbbbbb00-1111-2222-3333-444444444444",
			},
			want: "aaaaaa00-1111-2222-3333-444444444444",
		},
		{
			name: "falls back to CURSOR_CONVERSATION_ID when CURSOR_AGENT=1 and CERNE_SESSION empty",
			env: map[string]string{
				"CERNE_SESSION":          "",
				"CURSOR_AGENT":           "1",
				"CURSOR_CONVERSATION_ID": "bbbbbb00-1111-2222-3333-444444444444",
			},
			want: "bbbbbb00-1111-2222-3333-444444444444",
		},
		{
			name: "ignores CURSOR_CONVERSATION_ID without CURSOR_AGENT=1",
			env: map[string]string{
				"CERNE_SESSION":          "",
				"CURSOR_AGENT":           "",
				"CURSOR_CONVERSATION_ID": "bbbbbb00-1111-2222-3333-444444444444",
			},
			want: "",
		},
		{
			name: "empty when nothing set",
			env:  map[string]string{"CERNE_SESSION": "", "CURSOR_AGENT": "", "CURSOR_CONVERSATION_ID": ""},
			want: "",
		},
		{
			name: "rejects traversal in CERNE_SESSION",
			env: map[string]string{
				"CERNE_SESSION": "../escape",
			},
			want: "",
		},
		{
			name: "rejects path separator in CERNE_SESSION",
			env: map[string]string{
				"CERNE_SESSION": "a/b",
			},
			want: "",
		},
		{
			name: "rejects backslash in CERNE_SESSION",
			env: map[string]string{
				"CERNE_SESSION": `a\b`,
			},
			want: "",
		},
		{
			name: "rejects dot in CERNE_SESSION",
			env: map[string]string{
				"CERNE_SESSION": ".",
			},
			want: "",
		},
		{
			name: "rejects traversal in CURSOR_CONVERSATION_ID",
			env: map[string]string{
				"CURSOR_AGENT":           "1",
				"CURSOR_CONVERSATION_ID": "../escape",
			},
			want: "",
		},
		{
			name: "rejects path separator in CURSOR_CONVERSATION_ID",
			env: map[string]string{
				"CURSOR_AGENT":           "1",
				"CURSOR_CONVERSATION_ID": "a/b",
			},
			want: "",
		},
		{
			name: "rejects dot-dot in CURSOR_CONVERSATION_ID",
			env: map[string]string{
				"CURSOR_AGENT":           "1",
				"CURSOR_CONVERSATION_ID": "..",
			},
			want: "",
		},
		{
			name: "valid UUID as CURSOR_CONVERSATION_ID in Cursor shell",
			env: map[string]string{
				"CERNE_SESSION":          "",
				"CURSOR_AGENT":           "1",
				"CURSOR_CONVERSATION_ID": "98fdb738-ae20-43e9-b08b-1ac04f1b6f52",
			},
			want: "98fdb738-ae20-43e9-b08b-1ac04f1b6f52",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// A key a case leaves out is unset, not the host's (an agent
			// shell exports CERNE_SESSION).
			for _, k := range []string{"CERNE_SESSION", "CURSOR_AGENT", "CURSOR_CONVERSATION_ID"} {
				t.Setenv(k, "")
			}
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			got := CurrentSession()
			if got != tc.want {
				t.Errorf("CurrentSession() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestCurrentSessionTestMainClearsConversationID proves that TestMain already
// unsets CURSOR_CONVERSATION_ID so tests are hermetic when run from a Cursor
// agent shell. F-0525 crit. 5
func TestCurrentSessionTestMainClearsConversationID(t *testing.T) { // F-0525 crit. 5
	if v := os.Getenv("CURSOR_CONVERSATION_ID"); v != "" {
		t.Errorf("CURSOR_CONVERSATION_ID = %q; TestMain must unset it for hermetic tests", v)
	}
}
