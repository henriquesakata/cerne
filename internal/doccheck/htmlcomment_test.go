package doccheck

import "testing"

// B-0077: a "<!--" inside code is text, never a comment opener (CommonMark).
func TestStripHTMLComments(t *testing.T) {
	tests := []struct{ name, in, want string }{
		// B-0077 crit. 1: inside an inline code span, at any position.
		{"opener mid code span", "`a <!-- b` x\n\nkeep\n<!-- c -->\nend", "`a <!-- b` x\n\nkeep\n\nend"},
		{"opener at code span start", "`<!-- x -->` keep <!-- c --> end", "`<!-- x -->` keep  end"},
		{"double-backtick span", "``a ` <!-- b`` keep <!-- c --> end", "``a ` <!-- b`` keep  end"},
		// B-0077 crit. 2: inside a fenced block, backticks or tildes.
		{"backtick fence", "```\n<!-- a\n```\nkeep\n<!-- c -->\n", "```\n<!-- a\n```\nkeep\n\n"},
		{"tilde fence", "~~~md\nx <!-- a\n~~~\nkeep -->\n", "~~~md\nx <!-- a\n~~~\nkeep -->\n"},
		// B-0077 crit. 4: comments outside code go as before.
		{"multi-line template comment", "# T\n<!-- HOW TO USE:\nfill `x`\n-->\nbody", "# T\n\nbody"},
		{"cost anchor", "x\n<!-- cerne:cost v1 {\"a\":1} -->\n", "x\n\n"},
		{"comment holding a code span", "a <!-- use `Barra:` here --> b", "a  b"},
		{"comment holding a lone backtick", "a <!-- don't ` here --> b `c`", "a  b `c`"},
		{"comment holding a fence", "a\n<!--\n```\nx\n```\n-->\nb", "a\n\nb"},
		{"unclosed comment kept", "a <!-- b\nc", "a <!-- b\nc"},
		// B-0077 crit. 5: an unpaired backtick does not cross a blank line.
		{"unpaired backtick", "it's `x\n\n<!-- tpl -->\nok `y`", "it's `x\n\n\nok `y`"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := string(StripHTMLComments([]byte(tc.in))); got != tc.want {
				t.Errorf("StripHTMLComments(%q)\n got %q\nwant %q", tc.in, got, tc.want)
			}
		})
	}
}
