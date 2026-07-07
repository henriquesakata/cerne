package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/telemetry"
)

func newDoctorCmd() *cobra.Command {
	var timings bool
	var port int
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose a running cerne server",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !timings {
				return fmt.Errorf("nothing to diagnose — use --timings to list the server's recent operations")
			}
			return runDoctorTimings(cmd, port)
		},
	}
	cmd.Flags().BoolVar(&timings, "timings", false, "list the recent operations (phases, durations, retries) of the local server")
	cmd.Flags().IntVar(&port, "port", 7717, "port of the running cerne ui server")
	return cmd
}

// runDoctorTimings prints the server's recent-operations ring (F-0149): one
// line per operation, newest first, with its phase breakdown.
func runDoctorTimings(cmd *cobra.Command, port int) error {
	url := fmt.Sprintf("http://127.0.0.1:%d/api/debug/timings", port)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("no cerne server answering on port %d (start one with 'cerne ui'): %w", port, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server on port %d answered %d — is it an older cerne build without /api/debug/timings?", port, resp.StatusCode)
	}
	var records []telemetry.Record
	if err := json.NewDecoder(resp.Body).Decode(&records); err != nil {
		return fmt.Errorf("unexpected timings payload: %w", err)
	}
	if len(records) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No operations recorded yet.")
		return nil
	}
	out := cmd.OutOrStdout()
	for _, r := range records {
		line := fmt.Sprintf("%s  %-10s %-32s %8.0fms  %s", r.Start.Format("15:04:05"), r.Kind, r.Route, r.Ms, r.Status)
		if r.Retries > 0 {
			line += fmt.Sprintf("  retries=%d", r.Retries)
		}
		fmt.Fprintln(out, line)
		if len(r.Phases) > 0 {
			parts := make([]string, len(r.Phases))
			for i, p := range r.Phases {
				parts[i] = fmt.Sprintf("%s %.0fms", p.Name, p.Ms)
			}
			fmt.Fprintf(out, "          └ %s\n", strings.Join(parts, " · "))
		}
	}
	return nil
}
