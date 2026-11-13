package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/guide"
)

func newGuideCmd() *cobra.Command {
	var asJSON bool
	var limit int
	cmd := &cobra.Command{
		Use:   "guide [question]",
		Short: "Ask how to use cerne: the matching skill, help and FAQ sections",
		Long: "Answers a question about cerne itself from the binary's own corpus: the embedded " +
			"cerne-authoring skill, the help of every command and a FAQ. Offline and deterministic: it " +
			"reads no backend and calls no model. Prints up to --limit sections (default 3), each with " +
			"its title, source and text. With no question, or no match, it lists the topics. --json for " +
			"structured output.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			q := ""
			if len(args) == 1 {
				q = strings.TrimSpace(args[0])
			}
			corpus := guide.Corpus(cmd.Root())
			found := guide.Search(corpus, q, limit)
			out := cmd.OutOrStdout()
			if asJSON {
				if found == nil {
					found = []guide.Section{}
				}
				b, err := json.MarshalIndent(struct {
					Query    string          `json:"query"`
					Sections []guide.Section `json:"sections"`
				}{q, found}, "", "  ")
				if err != nil {
					return err
				}
				fmt.Fprintln(out, string(b))
				return nil
			}
			if len(found) == 0 {
				if q != "" {
					fmt.Fprintf(out, "No guide section matches %q.\n\n", q)
				}
				printTopics(out, corpus)
				return nil
			}
			for i, s := range found {
				if i > 0 {
					fmt.Fprintln(out)
				}
				fmt.Fprintf(out, "## %s  (%s)\n%s\n", s.Title, s.Source, s.Text)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "machine-readable JSON output")
	cmd.Flags().IntVar(&limit, "limit", 3, "maximum number of sections")
	return cmd
}

// printTopics lists what a question can be about: the skill sections and the
// FAQ questions. The command sections are left out; `cerne --help` lists them.
func printTopics(out io.Writer, corpus []guide.Section) {
	fmt.Fprintln(out, "Topics (ask with: cerne guide \"<topic>\"; any command: cerne guide \"<command name>\"):")
	for _, s := range corpus {
		if strings.HasPrefix(s.Source, "skill: ") || s.Source == "faq" {
			fmt.Fprintf(out, "  %s  (%s)\n", s.Title, s.Source)
		}
	}
}
