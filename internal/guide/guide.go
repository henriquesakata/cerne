// Package guide is the offline corpus behind `cerne guide` (F-0398): the
// embedded skill, the cobra command tree and an embedded FAQ, searched in
// memory. No backend, no network, no model.
package guide

import (
	_ "embed"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/skills"
)

// Section is one answerable unit of the corpus.
type Section struct {
	Title  string `json:"title"`
	Source string `json:"source"` // "skill: <section>", "reference: <file>", "help: cerne <path>" or "faq"
	Text   string `json:"text"`
}

//go:embed faq.md
var faq string

// authoring is the embedded skill the corpus reads: its SKILL.md and, since
// F-0399, the reference files beside it.
const authoring = "cerne-authoring"

// Corpus joins the skill sections, one section per visible command of root
// and the FAQ entries.
func Corpus(root *cobra.Command) []Section {
	var out []Section
	for _, sk := range skills.All() {
		if sk.Name != authoring {
			continue
		}
		out = append(out, skillSections(sk.RelPath, sk.Content)...)
		for _, r := range sk.References {
			out = append(out, skillSections(sk.Name+"/"+r.RelPath, r.Content)...)
		}
	}
	out = append(out, commandSections(root)...)
	return append(out, faqSections(faq)...)
}

// emptyText stands in for a section with no body of its own (a ## that only
// introduces its ### children), so its title still reaches the topics list.
const emptyText = "(no text of its own: see the sections below it)"

// skillSections splits a markdown file at its # title and every ## and ###
// heading (outside code fences); a section runs to the next heading. The text
// before the first ## is the intro section, titled by the # heading. A section
// of SKILL.md has the source "skill: <title>", one of a reference file
// "reference: <file>".
func skillSections(path, content string) []Section {
	return splitHeadings(content, []string{"### ", "## ", "# "}, func(title string) (string, string) {
		if base := filepath.Base(path); base != "SKILL.md" {
			return title, "reference: " + base
		}
		return title, "skill: " + title
	})
}

// faqSections reads "## Q: <question>" entries.
func faqSections(content string) []Section {
	return splitHeadings(content, []string{"## Q: "}, func(title string) (string, string) { return title, "faq" })
}

func splitHeadings(content string, prefixes []string, name func(string) (string, string)) []Section {
	var out []Section
	var cur *Section
	var body []string
	flush := func() {
		if cur != nil {
			cur.Text = strings.TrimSpace(strings.Join(body, "\n"))
			if cur.Text == "" {
				cur.Text = emptyText
			}
			out = append(out, *cur)
		}
		cur, body = nil, nil
	}
	fence := "" // the marker (``` or ~~~) of the open code fence
	for _, line := range strings.Split(content, "\n") {
		if m := strings.TrimLeft(line, " "); strings.HasPrefix(m, "```") || strings.HasPrefix(m, "~~~") {
			if fence == "" {
				fence = m[:3]
			} else if strings.HasPrefix(m, fence) {
				fence = ""
			}
		} else if fence == "" {
			if title, ok := headingTitle(line, prefixes); ok {
				flush()
				t, src := name(title)
				cur = &Section{Title: t, Source: src}
				continue
			}
		}
		body = append(body, line)
	}
	flush()
	return out
}

func headingTitle(line string, prefixes []string) (string, bool) {
	for _, p := range prefixes {
		if strings.HasPrefix(line, p) {
			return strings.TrimSpace(strings.TrimPrefix(line, p)), true
		}
	}
	return "", false
}

// commandSections is one section per visible command below root.
func commandSections(root *cobra.Command) []Section {
	var out []Section
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		for _, sub := range c.Commands() {
			if sub.Hidden || sub.Name() == "help" {
				continue
			}
			text := sub.Use + "\n" + sub.Short
			if sub.Long != "" {
				text += "\n\n" + sub.Long
			}
			if len(sub.Aliases) > 0 {
				text += "\n\nAliases: " + strings.Join(sub.Aliases, ", ")
			}
			if sub.Deprecated != "" {
				text += "\n\nDeprecated: " + sub.Deprecated
			}
			if fl := sub.LocalFlags().FlagUsages(); fl != "" {
				text += "\n\nFlags:\n" + fl
			}
			out = append(out, Section{Title: sub.CommandPath(), Source: "help: " + sub.CommandPath(), Text: text})
			walk(sub)
		}
	}
	walk(root)
	return out
}
