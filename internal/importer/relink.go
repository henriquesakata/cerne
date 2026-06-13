package importer

import (
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// Link rewriting turns the cross-references of imported docs — written against
// the original hub tree — into links that resolve in the cerne backend:
//
//   - Obsidian wikilinks [[slug]] / [[slug#h]] / [[slug|alias]] and inline
//     markdown links [t](../old/path.md) become relative links to the new
//     content path of the target entity ([t](../decisions/D-0008.md));
//   - source-code references (apps/…, packages/…) become permalinks pinned to
//     the source repo's blob base, keeping a :NN line as #LNN.
//
// Anything that does not resolve to a migrated entity (or, for code, to a file
// present in the source tree) is left untouched. Fenced code blocks are
// skipped so example links inside them stay literal.

var (
	wikiRx    = regexp.MustCompile(`(!?)\[\[([^\]\n]+)\]\]`)
	mdLinkRx  = regexp.MustCompile(`\[([^\]]*)\]\(([^)]+)\)`)
	fenceRx   = regexp.MustCompile("^\\s*(```|~~~)")
	lineNumRx = regexp.MustCompile(`:(\d+)$`)
	// codeDirRx marks a target pointing into a source tree (hub monorepo roots
	// plus subsystem-local api/web folders).
	codeDirRx = regexp.MustCompile(`(^|/)(apps|packages|infra|scripts|config|certs|api|web|web-shared)/`)
)

// LinkIndex resolves an original-hub link target to the new content path of
// the entity it now lives in.
type LinkIndex struct {
	byPath map[string]string   // original path without extension -> content rel (slash)
	stems  map[string][]string // last path segment -> distinct content rels (unique-stem fallback)
	titles map[string]string   // content rel -> entity title (default wikilink text)
}

// NewLinkIndex returns an empty index.
func NewLinkIndex() *LinkIndex {
	return &LinkIndex{
		byPath: map[string]string{},
		stems:  map[string][]string{},
		titles: map[string]string{},
	}
}

// Add registers every original-path form of candidate c (now entity id) so a
// later link to any of those forms resolves to the right content document.
func (x *LinkIndex) Add(c Candidate, id string) {
	switch c.Kind {
	case "feature":
		for _, name := range featureDocNames {
			if _, ok := c.Files[name]; !ok {
				continue
			}
			rel := "content/features/" + id + "/" + name
			stem := strings.TrimSuffix(name, ".md")
			orig := strings.TrimSuffix(c.Source, "/") + "/" + stem // docs/process/cases/0001_x/spec
			x.put(orig, rel, c.Title)
			x.put(lastN(orig, 2), rel, c.Title) // 0001_x/spec
			if stem == "spec" {
				// A bare reference to the case folder means its spec.
				caseDir := strings.TrimSuffix(c.Source, "/") // docs/process/cases/0001_x
				x.put(caseDir, rel, c.Title)
				x.put(path.Base(caseDir), rel, c.Title) // 0001_x
			}
		}
	case "decision", "reference":
		sub := "decisions"
		if c.Kind == "reference" {
			sub = "references"
		}
		rel := "content/" + sub + "/" + id + ".md"
		orig := strings.TrimSuffix(c.Source, ".md")
		x.put(orig, rel, c.Title)
		x.put(path.Base(orig), rel, c.Title)
	}
}

// put records key -> contentRel (first writer wins) and the stem fallback.
func (x *LinkIndex) put(key, contentRel, title string) {
	key = cleanSlash(strings.TrimPrefix(key, "docs/"))
	if key == "" {
		return
	}
	if _, ok := x.byPath[key]; !ok {
		x.byPath[key] = contentRel
	}
	stem := lastN(key, 1)
	if !contains(x.stems[stem], contentRel) {
		x.stems[stem] = append(x.stems[stem], contentRel)
	}
	if title != "" {
		x.titles[contentRel] = title
	}
}

// resolve maps a link target (as written in the doc whose original location is
// fromSource) to the target's content rel path; ok is false when no migrated
// entity matches.
func (x *LinkIndex) resolve(fromSource, target string) (string, bool) {
	target = strings.TrimSuffix(strings.TrimSpace(target), ".md")
	if target == "" {
		return "", false
	}
	var cands []string
	if dir := sourceDir(fromSource); dir != "" {
		cands = append(cands, cleanJoin(dir, target))
	}
	cands = append(cands, cleanSlash(target))
	for t := target; strings.HasPrefix(t, "../"); {
		t = t[3:]
		cands = append(cands, cleanSlash(t))
	}
	for _, c := range cands {
		c = strings.TrimPrefix(c, "docs/")
		if rel, ok := x.byPath[c]; ok {
			return rel, true
		}
	}
	if rels := x.stems[lastN(target, 1)]; len(rels) == 1 {
		return rels[0], true
	}
	return "", false
}

// PermalinkBase turns a source-tree path into a blob permalink. A zero value
// (empty BlobBase) disables code-reference rewriting.
type PermalinkBase struct {
	BlobBase string // e.g. https://gitlab.com/rockt/hub/-/blob/<sha>
	SrcRoot  string // source tree root, to verify the file still exists
}

// BlobBase builds the blob-URL prefix that permalinks a source file pinned at
// sha, from the source repo's remote URL. It supports GitLab and GitHub hosts
// and returns "" when the host is unrecognised or an input is empty, which
// disables code-reference rewriting.
func BlobBase(remote, sha string) string {
	if remote == "" || sha == "" {
		return ""
	}
	host, repoPath, ok := parseGitRemote(remote)
	if !ok {
		return ""
	}
	switch {
	case strings.Contains(host, "gitlab"):
		return "https://" + host + "/" + repoPath + "/-/blob/" + sha
	case strings.Contains(host, "github"):
		return "https://" + host + "/" + repoPath + "/blob/" + sha
	default:
		return ""
	}
}

// parseGitRemote extracts the host and "owner/repo" path from the common git
// remote forms: git@host:owner/repo(.git), https://host/owner/repo(.git),
// ssh://git@host[:port]/owner/repo(.git).
func parseGitRemote(remote string) (host, repoPath string, ok bool) {
	remote = strings.TrimSuffix(strings.TrimSpace(remote), ".git")
	if rest, found := strings.CutPrefix(remote, "git@"); found {
		i := strings.IndexByte(rest, ':')
		if i < 0 {
			return "", "", false
		}
		return rest[:i], strings.TrimPrefix(rest[i+1:], "/"), true
	}
	for _, scheme := range []string{"https://", "http://", "ssh://"} {
		rest, found := strings.CutPrefix(remote, scheme)
		if !found {
			continue
		}
		if at := strings.IndexByte(rest, '@'); at >= 0 {
			rest = rest[at+1:]
		}
		i := strings.IndexByte(rest, '/')
		if i < 0 {
			return "", "", false
		}
		host = rest[:i]
		if c := strings.IndexByte(host, ':'); c >= 0 {
			host = host[:c] // drop an ssh port
		}
		return host, strings.TrimPrefix(rest[i+1:], "/"), true
	}
	return "", "", false
}

// codeURL returns the permalink for a source-code target written in the doc at
// fromSource, or ok=false when it is not a code path or the file is absent.
func (pb PermalinkBase) codeURL(fromSource, target string) (string, bool) {
	if pb.BlobBase == "" {
		return "", false
	}
	line := ""
	if m := lineNumRx.FindStringSubmatch(target); m != nil {
		line = m[1]
		target = target[:len(target)-len(m[0])]
	}
	if !codeDirRx.MatchString(target) {
		return "", false
	}
	rel := pb.hubPath(fromSource, target)
	if rel == "" {
		return "", false
	}
	url := pb.BlobBase + "/" + rel
	if line != "" {
		url += "#L" + line
	}
	return url, true
}

// hubPath finds the source-root-relative path of a code target, trying the
// doc-relative and root-relative readings, and returns "" when none exists on
// disk.
func (pb PermalinkBase) hubPath(fromSource, target string) string {
	var cands []string
	if dir := sourceDir(fromSource); dir != "" {
		cands = append(cands, cleanJoin(dir, target))
	}
	cands = append(cands, cleanSlash(target))
	for t := target; strings.HasPrefix(t, "../"); {
		t = t[3:]
		cands = append(cands, cleanSlash(t))
	}
	for _, c := range cands {
		c = strings.TrimPrefix(c, "docs/")
		if c == "" {
			continue
		}
		if fi, err := os.Stat(filepath.Join(pb.SrcRoot, filepath.FromSlash(c))); err == nil && !fi.IsDir() {
			return c
		}
	}
	return ""
}

// rewriteLinks rewrites the cross-references in body. fromSource is the doc's
// original hub path; fromContentRel is its new content rel path (its directory
// anchors the relative links). Original line endings are preserved.
func rewriteLinks(body []byte, fromSource, fromContentRel string, idx *LinkIndex, pb PermalinkBase) []byte {
	crlf := strings.Contains(string(body), "\r\n")
	text := strings.ReplaceAll(string(body), "\r\n", "\n")
	fromDir := path.Dir(fromContentRel)
	lines := strings.Split(text, "\n")
	fenced := false
	for i, line := range lines {
		if fenceRx.MatchString(line) {
			fenced = !fenced
			continue
		}
		if fenced {
			continue
		}
		// Inline markdown links first, so links synthesised from wikilinks in
		// the second pass are not re-examined.
		if strings.Contains(line, "](") {
			line = mdLinkRx.ReplaceAllStringFunc(line, func(m string) string {
				return rewriteMD(m, fromSource, fromDir, idx, pb)
			})
		}
		if strings.Contains(line, "[[") {
			line = wikiRx.ReplaceAllStringFunc(line, func(m string) string {
				return rewriteWiki(m, fromSource, fromDir, idx)
			})
		}
		lines[i] = line
	}
	out := strings.Join(lines, "\n")
	if crlf {
		out = strings.ReplaceAll(out, "\n", "\r\n")
	}
	return []byte(out)
}

func rewriteMD(match, fromSource, fromDir string, idx *LinkIndex, pb PermalinkBase) string {
	m := mdLinkRx.FindStringSubmatch(match)
	if m == nil {
		return match
	}
	text, raw := m[1], strings.TrimSpace(m[2])
	inner := raw
	if sp := strings.IndexByte(inner, ' '); sp >= 0 && !strings.HasPrefix(inner, "#") {
		inner = inner[:sp] // drop a (url "title") suffix
	}
	if inner == "" || isExternalOrAnchor(inner) {
		return match
	}
	target, frag := splitFrag(inner)
	if rel, ok := idx.resolve(fromSource, target); ok {
		return "[" + text + "](" + relPath(fromDir, rel) + frag + ")"
	}
	if url, ok := pb.codeURL(fromSource, target); ok {
		return "[" + text + "](" + url + ")"
	}
	return match
}

func rewriteWiki(match, fromSource, fromDir string, idx *LinkIndex) string {
	m := wikiRx.FindStringSubmatch(match)
	if m == nil || m[1] == "!" {
		return match // image-style ![[...]] left as-is
	}
	inner := m[2]
	target, alias := inner, ""
	if i := strings.IndexByte(inner, '|'); i >= 0 {
		target, alias = inner[:i], inner[i+1:]
	}
	target, frag := splitFrag(target)
	target = strings.TrimSpace(target)
	if target == "" {
		return match // same-page [[#heading]] anchor
	}
	rel, ok := idx.resolve(fromSource, target)
	if !ok {
		return match
	}
	disp := strings.TrimSpace(alias)
	if disp == "" {
		disp = idx.titles[rel]
	}
	if disp == "" {
		disp = lastN(target, 1)
	}
	p := relPath(fromDir, rel)
	if frag != "" {
		p += frag
	}
	return "[" + disp + "](" + p + ")"
}

// --- small path/string helpers ---

func splitFrag(s string) (base, frag string) {
	if i := strings.IndexByte(s, '#'); i >= 0 {
		return s[:i], s[i:]
	}
	return s, ""
}

func isExternalOrAnchor(target string) bool {
	low := strings.ToLower(target)
	return strings.HasPrefix(low, "http://") ||
		strings.HasPrefix(low, "https://") ||
		strings.HasPrefix(low, "mailto:") ||
		strings.HasPrefix(target, "#") ||
		strings.HasPrefix(target, "/")
}

// sourceDir is the directory a doc at source lived in: the case folder itself
// for features (trailing slash), the file's parent otherwise.
func sourceDir(source string) string {
	if strings.HasSuffix(source, "/") {
		return strings.TrimSuffix(source, "/")
	}
	return path.Dir(source)
}

func cleanSlash(p string) string {
	if p == "" {
		return ""
	}
	return path.Clean(p)
}

func cleanJoin(dir, rel string) string {
	return path.Clean(dir + "/" + rel)
}

// relPath returns target (a repo-rel slash path) relative to fromDir (also
// repo-rel slash), e.g. ("content/features/F-0001", "content/decisions/D-0008.md")
// -> "../../decisions/D-0008.md".
func relPath(fromDir, target string) string {
	r, err := filepath.Rel(filepath.FromSlash(fromDir), filepath.FromSlash(target))
	if err != nil {
		return target
	}
	return filepath.ToSlash(r)
}

// lastN returns the last n slash segments of p.
func lastN(p string, n int) string {
	segs := strings.Split(p, "/")
	if len(segs) <= n {
		return p
	}
	return strings.Join(segs[len(segs)-n:], "/")
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
