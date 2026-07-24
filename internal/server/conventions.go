package server

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"slices"
	"strings"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

// handleConventionsExport renders the active notes tagged with both the
// "convention" and "platform" components as one markdown document, for
// runtime distribution to external project harnesses (hub F-0471/F-0474).
// Consumers filter by the per-section components marker, so notes without
// the platform component never leave this server. Sections are ordered by
// note ID (LoadAllNotes order) so the output — and its ETag — are stable
// for a given catalog state.
func handleConventionsExport(w http.ResponseWriter, repoDir string) {
	notes, err := store.LoadAllNotes(repoDir)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	var b strings.Builder
	for _, n := range notes {
		if n.State != domain.NoteActive {
			continue
		}
		if !slices.Contains(n.Components, "convention") || !slices.Contains(n.Components, "platform") {
			continue
		}
		doc, err := readContentDoc(repoDir, "notes", n.ID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
			return
		}
		b.WriteString("## " + n.ID + " — " + n.Title + "\n")
		b.WriteString("<!-- components: " + strings.Join(n.Components, ",") + " -->\n\n")
		if body := strings.TrimSpace(stripLeadingFrontmatterBlock(doc)); body != "" {
			b.WriteString(body + "\n")
		}
		b.WriteString("\n")
	}
	out := b.String()
	sum := sha256.Sum256([]byte(out))
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("ETag", `"`+hex.EncodeToString(sum[:8])+`"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(out))
}

// stripLeadingFrontmatterBlock removes a leading "---" block (mirrors
// internal/ask.stripFrontmatter, which is package-private there).
func stripLeadingFrontmatterBlock(s string) string {
	if !strings.HasPrefix(strings.TrimLeft(s, " \t\n"), "---") {
		return s
	}
	lines := strings.Split(s, "\n")
	start := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == "---" {
			start = i
			break
		}
	}
	for i := start + 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			return strings.TrimLeft(strings.Join(lines[i+1:], "\n"), "\n")
		}
	}
	return s
}
