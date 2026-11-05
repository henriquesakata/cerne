package cli

import (
	"encoding/json"
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/project"
)

// newSettingsCmd is the CLI face of the settings registry (F-0314): the same
// ResolveSettings/SetSetting/UnsetSetting the ui's /settings endpoint calls.
func newSettingsCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "settings",
		Short: "Read and write cerne settings (project > host > default)",
		Long: "Every key is declared in one registry. Its value resolves project (.cernerc) > host " +
			"(settings.toml in the user config dir) > the declared default, and the winning scope is reported.",
	}
	cmd.PersistentFlags().StringVar(&dir, "dir", ".", "project directory")
	cmd.AddCommand(newSettingsListCmd(&dir), newSettingsGetCmd(&dir), newSettingsSetCmd(&dir), newSettingsUnsetCmd(&dir))
	return cmd
}

func newSettingsListCmd(dir *string) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List every setting with its value, default and origin scope",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			view, err := project.ResolveSettings(*dir)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if asJSON {
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(view)
			}
			w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "KEY\tTYPE\tVALUE\tDEFAULT\tORIGIN")
			for _, s := range view.Settings {
				fmt.Fprintf(w, "%s\t%s\t%v\t%v\t%s\n", s.ID, s.Type, s.Value, s.Default, s.Origin)
			}
			w.Flush()
			fmt.Fprintln(out)
			for _, scope := range []string{project.ScopeProject, project.ScopeHost} {
				if f, ok := view.Files[scope]; ok {
					fmt.Fprintf(out, "%s: %s\n", scope, f)
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the registry with resolved values as JSON")
	return cmd
}

func newSettingsGetCmd(dir *string) *cobra.Command {
	var showOrigin bool
	cmd := &cobra.Command{
		Use:   "get <key>",
		Short: "Print a setting's effective value",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := resolvedSetting(*dir, args[0])
			if err != nil {
				return err
			}
			if showOrigin {
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t", s.Origin)
			}
			fmt.Fprintln(cmd.OutOrStdout(), s.Value)
			return nil
		},
	}
	cmd.Flags().BoolVar(&showOrigin, "show-origin", false, "prefix the value with the scope it came from")
	return cmd
}

func newSettingsSetCmd(dir *string) *cobra.Command {
	var scope string
	cmd := &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Write a setting in one scope",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := project.SetSetting(*dir, args[0], scope, args[1]); err != nil {
				return err
			}
			s, file, err := writtenSetting(*dir, args[0], scope)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "set %s = %s in %s scope (%s)\n", args[0], args[1], scope, file)
			if s.Origin != scope {
				// Only the project scope outranks another: say why the value did not take.
				fmt.Fprintf(out, "note: overridden by %s scope (effective: %v)\n", s.Origin, s.Value)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&scope, "scope", "", "where to write: project (.cernerc) or host (settings.toml)")
	cmd.MarkFlagRequired("scope")
	return cmd
}

func newSettingsUnsetCmd(dir *string) *cobra.Command {
	var scope string
	cmd := &cobra.Command{
		Use:   "unset <key>",
		Short: "Remove a setting from one scope",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := project.UnsetSetting(*dir, args[0], scope); err != nil {
				return err
			}
			s, file, err := writtenSetting(*dir, args[0], scope)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "unset %s from %s scope (%s)\neffective: %v (%s)\n", args[0], scope, file, s.Value, s.Origin)
			return nil
		},
	}
	cmd.Flags().StringVar(&scope, "scope", "", "where to remove it from: project (.cernerc) or host (settings.toml)")
	cmd.MarkFlagRequired("scope")
	return cmd
}

// writtenSetting re-resolves id after a write and returns it with the file
// behind scope.
func writtenSetting(dir, id, scope string) (project.ResolvedSetting, string, error) {
	view, err := project.ResolveSettings(dir)
	if err != nil {
		return project.ResolvedSetting{}, "", err
	}
	s, err := findResolved(view, id)
	return s, view.Files[scope], err
}

func resolvedSetting(dir, id string) (project.ResolvedSetting, error) {
	view, err := project.ResolveSettings(dir)
	if err != nil {
		return project.ResolvedSetting{}, err
	}
	return findResolved(view, id)
}

func findResolved(view project.SettingsView, id string) (project.ResolvedSetting, error) {
	for _, s := range view.Settings {
		if s.ID == id {
			return s, nil
		}
	}
	return project.ResolvedSetting{}, fmt.Errorf("%w %q", project.ErrUnknownSetting, id)
}
