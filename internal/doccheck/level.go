package doccheck

import (
	"regexp"
	"strings"

	"github.com/henriquesakata/cerne/internal/doclang"
)

// levelLineRe is the spec's level metadata line, bold or plain, in either
// language: group 1 the first word of the value, group 2 the rest.
var levelLineRe = regexp.MustCompile(`^[ \t>*_-]*(?:N(?:í|i\x{0301}|i)vel|Level)[*_]*[ \t]*:[*_ \t]*([\pL\pM]+)(.*)$`)

// levels maps a folded level word, in either language, to its pt-BR name.
var levels = map[string]string{
	"leve": "leve", "light": "leve",
	"padrao": "padrão", "standard": "padrão",
	"avancado": "avançado", "advanced": "avançado",
}

// foldAccents drops the accents a level word carries, precomposed or
// decomposed (a paste from macOS arrives decomposed).
var foldAccents = strings.NewReplacer("ã", "a", "ç", "c", "̃", "", "̧", "")

// SpecLevel reads the spec's **Nível:** / **Level:** metadata line (F-0390),
// normalized to leve, padrão or avançado; the English values are accepted
// under either label. The first such line outside comments and code decides:
// a template slot ("leve | padrão | avançado"), a compound ("leve-padrão")
// or another word is no level, and neither is a missing line.
func SpecLevel(body []byte) (string, bool) {
	inCode := false
	for _, line := range strings.Split(htmlCommentRe.ReplaceAllString(string(body), ""), "\n") {
		if doclang.Fence(line) {
			inCode = !inCode
			continue
		}
		m := levelLineRe.FindStringSubmatch(line)
		if inCode || m == nil {
			continue
		}
		if strings.HasPrefix(m[2], "-") || strings.HasPrefix(strings.TrimLeft(m[2], " \t*_"), "|") {
			return "", false
		}
		l, ok := levels[foldAccents.Replace(strings.ToLower(m[1]))]
		return l, ok
	}
	return "", false
}
