package doccheck

import (
	"errors"
	"testing"

	"github.com/henriquesakata/cerne/internal/doclang"
)

// TestSpecBarIn_CauseFollowsLanguage (F-0418 crit. 3): a missing bar is
// named in the project's language; a malformed one by the label the author
// wrote. The sentinels still match through errors.Is.
func TestSpecBarIn_CauseFollowsLanguage(t *testing.T) {
	const slot = ` holds only a <…> template placeholder, not a named reference`
	tests := []struct {
		name     string
		lang     doclang.Lang
		verif    string
		want     string
		sentinel error
	}{
		{"missing, en project", doclang.EN, "Unit test.", `no "Bar:" line`, ErrBarMissing},
		{"missing, pt project", doclang.PT, "Teste unitário.", `no "Barra:" line`, ErrBarMissing},
		{"slot, en", doclang.EN, "Bar: <named reference>", `malformed "Bar:" line: "Bar:"` + slot, ErrBarMalformed},
		{"slot, pt", doclang.PT, "Barra: <referência nomeada>", `malformed "Barra:" line: "Barra:"` + slot, ErrBarMalformed},
		{"en line in a pt project", doclang.PT, "Bar: <named reference>", `malformed "Bar:" line: "Bar:"` + slot, ErrBarMalformed},
		{"pt line in an en project", doclang.EN, "Barra: <referência nomeada>", `malformed "Barra:" line: "Barra:"` + slot, ErrBarMalformed},
		{"exemption without justification, en", doclang.EN, "Bar: not applicable", `malformed "Bar:" line: "not applicable" needs its one-line justification in parentheses`, ErrBarMalformed},
		{"waiver without quote, en", doclang.EN, "Bar: waived by human decision", `malformed "Bar:" line: "waived by human decision" needs the quoted words in parentheses`, ErrBarMalformed},
		{"exemption with a slot, pt", doclang.PT, "Barra: não aplicável (<justificativa>)", `malformed "Barra:" line: "não aplicável" needs its one-line justification in parentheses, not a <…> template placeholder`, ErrBarMalformed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := SpecBarIn([]byte("## Verification\n\n"+tt.verif+"\n"), tt.lang)
			if err == nil || err.Error() != tt.want {
				t.Fatalf("SpecBarIn(%q, %s) = %v, want %q", tt.verif, tt.lang, err, tt.want)
			}
			if !errors.Is(err, tt.sentinel) {
				t.Fatalf("errors.Is(%v, %v) = false", err, tt.sentinel)
			}
		})
	}
}

// TestSpecBar_StaysPortuguese: SpecBar, the language-blind entry the result
// reader and older callers use, keeps naming the Portuguese label.
func TestSpecBar_StaysPortuguese(t *testing.T) {
	if _, err := SpecBar([]byte("## Verification\n\nUnit test.\n")); err == nil || err.Error() != `no "Barra:" line` {
		t.Fatalf("SpecBar = %v, want the Portuguese cause", err)
	}
}
