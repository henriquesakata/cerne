package cli

import "testing"

// TestSettingTextRecords: a records value prints as the compact JSON that
// 'settings set' takes back; a scalar prints as before.
func TestSettingTextRecords(t *testing.T) {
	for _, tc := range []struct {
		v    any
		want string
	}{
		{[]map[string]string{{"name": "gh", "kind": "github"}}, `[{"kind":"github","name":"gh"}]`},
		{[]map[string]string{}, `[]`},
		{true, "true"},
		{"accept", "accept"},
	} {
		if got := settingText(tc.v); got != tc.want {
			t.Errorf("settingText(%#v) = %s, want %s", tc.v, got, tc.want)
		}
	}
}
