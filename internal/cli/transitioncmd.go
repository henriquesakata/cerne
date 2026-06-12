package cli

import (
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/store"
)

// transitionSpec wires one lifecycle subcommand to its domain action.
type transitionSpec struct {
	use       string
	short     string
	action    domain.Action
	eventType string
}

var transitionSpecs = []transitionSpec{
	{"approve", "Approve a candidate feature (requires a real spec)", domain.ActionApprove, "feature.approved"},
	{"reject", "Reject a candidate feature (requires --reason)", domain.ActionReject, "feature.rejected"},
	{"start", "Start development (requires a real plan)", domain.ActionStart, "feature.started"},
	{"complete", "Complete development (requires a real result)", domain.ActionComplete, "feature.completed"},
	{"supersede", "Supersede a feature by another one (requires --by and --reason)", domain.ActionSupersede, "feature.superseded"},
}

func newTransitionCmds(dir *string) []*cobra.Command {
	cmds := make([]*cobra.Command, 0, len(transitionSpecs))
	for _, ts := range transitionSpecs {
		var by, reason string
		cmd := &cobra.Command{
			Use:   ts.use + " <id>",
			Short: ts.short,
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				return runTransition(cmd, *dir, ts, args[0], by, reason)
			},
		}
		cmd.Flags().StringVar(&by, "by", "", "id of the superseding feature (supersede only)")
		cmd.Flags().StringVar(&reason, "reason", "", "reason for the transition (reject and supersede)")
		cmds = append(cmds, cmd)
	}
	return cmds
}

func runTransition(cmd *cobra.Command, dir string, ts transitionSpec, id, by, reason string) error {
	if err := validateTransitionFlags(ts.action, by, reason); err != nil {
		return err
	}
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	f, err := store.LoadFeature(ctx.repoDir, id)
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("feature %s não encontrada no backend", id)
	}
	if err != nil {
		return err
	}
	if ts.action == domain.ActionSupersede {
		if by == id {
			return fmt.Errorf("--by não pode ser a própria feature (%s)", id)
		}
		if _, err := store.LoadFeature(ctx.repoDir, by); errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("feature %s (--by) não encontrada no backend", by)
		} else if err != nil {
			return err
		}
	}

	newState, err := domain.Transition(f.State, ts.action)
	if err != nil {
		return err
	}
	docs, err := featureDocSet(ctx.repoDir, id)
	if err != nil {
		return err
	}
	if err := domain.CheckGate(ts.action, docs); err != nil {
		return fmt.Errorf("%w — escreva content/features/%s/%s no backend e rode 'cerne push'", err, id, gateDocFilename(err))
	}

	oldState := f.State
	f.State = newState
	f.UpdatedAt = time.Now().UTC()
	switch ts.action {
	case domain.ActionSupersede:
		f.SupersededBy = by
		f.Reason = reason
	case domain.ActionReject:
		f.Reason = reason
	}
	if err := store.SaveFeature(ctx.repoDir, f); err != nil {
		return err
	}

	var payload map[string]string
	if by != "" || reason != "" {
		payload = map[string]string{}
		if by != "" {
			payload["by"] = by
		}
		if reason != "" {
			payload["reason"] = reason
		}
	}
	if err := store.AppendEvent(ctx.repoDir, domain.NewEvent(id, ts.eventType, payload, eventActor())); err != nil {
		return err
	}
	sha, err := gitback.CommitAndPush(ctx.repoDir, fmt.Sprintf("cerne: %s %s", ts.action, id))
	if err != nil {
		return err
	}
	if err := updateLockSnapshot(ctx.projDir, sha); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s: %s → %s\n", id, oldState, newState)
	return nil
}

// validateTransitionFlags enforces which flags each action accepts:
// supersede requires both, reject requires --reason only, every other
// action forbids both.
func validateTransitionFlags(action domain.Action, by, reason string) error {
	switch action {
	case domain.ActionSupersede:
		if by == "" {
			return fmt.Errorf("--by é obrigatório para supersede")
		}
		if reason == "" {
			return fmt.Errorf("--reason é obrigatório para supersede")
		}
	case domain.ActionReject:
		if by != "" {
			return fmt.Errorf("--by não é permitido para reject")
		}
		if reason == "" {
			return fmt.Errorf("--reason é obrigatório para reject")
		}
	default:
		if by != "" || reason != "" {
			return fmt.Errorf("--by/--reason não são permitidos para %s", action)
		}
	}
	return nil
}

// gateDocFilename maps a gate error to the document the hint should name.
func gateDocFilename(gateErr error) string {
	switch {
	case errors.Is(gateErr, domain.ErrGatePlanRequired):
		return "plan.md"
	case errors.Is(gateErr, domain.ErrGateResultRequired):
		return "result.md"
	default:
		return "spec.md"
	}
}
