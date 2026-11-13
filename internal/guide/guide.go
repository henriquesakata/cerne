// Package guide is the offline corpus behind `cerne guide` (F-0398): the
// embedded skill, the cobra command tree and an embedded FAQ, searched in
// memory. No backend, no network, no model.
package guide

import (
	_ "embed"
	"strings"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/skills"
)

// Section is one answerable unit of the corpus.
type Section struct {
	Title  string `json:"title"`
	Source string `json:"source"` // "skill: <section>", "help: cerne <path>" or "faq"
	Text   string `json:"text"`
}

//go:embed faq.md
var faq string

// skillFiles are the embedded skill files the corpus reads; F-0399 adds its
// reference files here.
var skillFiles = []string{"cerne-authoring/SKILL.md"}

// Corpus joins the skill sections, one section per visible command of root
// and the FAQ entries.
func Corpus(root *cobra.Command) []Section {
	var out []Section
	for _, f := range skillFiles {
		if content, ok := skills.Read(f); ok {
			out = append(out, skillSections(f, content)...)
		}
	}
	out = append(out, commandSections(root)...)
	return append(out, faqSections(faq)...)
}

// skillSections splits a markdown file at every ## and ### heading (outside
// code fences); a section runs to the next such heading.
func skillSections(_, content string) []Section {
	return splitHeadings(content, "## ", "### ", func(title string) (string, string) { return title, "skill: " + title })
}

// faqSections reads "## Q: <question>" entries.
func faqSections(content string) []Section {
	return splitHeadings(content, "## Q: ", "", func(title string) (string, string) { return title, "faq" })
}

func splitHeadings(content, h2, h3 string, name func(string) (string, string)) []Section {
	var out []Section
	var cur *Section
	var body []string
	flush := func() {
		if cur != nil {
			cur.Text = strings.TrimSpace(strings.Join(body, "\n"))
			if cur.Text != "" {
				out = append(out, *cur)
			}
		}
		cur, body = nil, nil
	}
	fence := false
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "```") {
			fence = !fence
		}
		if !fence {
			var title string
			switch {
			case strings.HasPrefix(line, h2):
				title = strings.TrimPrefix(line, h2)
			case h3 != "" && strings.HasPrefix(line, h3):
				title = strings.TrimPrefix(line, h3)
			}
			if title != "" {
				flush()
				t, src := name(strings.TrimSpace(title))
				cur = &Section{Title: t, Source: src}
				continue
			}
		}
		body = append(body, line)
	}
	flush()
	return out
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
