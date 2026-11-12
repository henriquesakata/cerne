package app

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
)

// Delivery carries the already-resolved URLs of the commit/MR that delivered
// a feature (F-0179). Fields are optional; the zero value means "no delivery
// captured".
type Delivery struct {
	CommitURL string
	MRURL     string
}

// ErrDeliveryInvalid is the sentinel for a --commit/--mr value that is
// neither an http(s) URL nor (for --commit) a bare SHA.
var ErrDeliveryInvalid = errors.New("invalid delivery link")

// ErrDeliveryNoTemplate is the sentinel for a bare commit SHA given without a
// usable [delivery] commit_url template in .cernerc.
var ErrDeliveryNoTemplate = errors.New("no [delivery] commit_url template configured")

// ErrDeliveryState is the sentinel for a post-hoc delivery update on a
// feature whose state does not accept one: only in_development, implemented
// and done do (done takes the data only, F-0355) — rejected/superseded stay
// closed and pre-development states have nothing delivered yet.
var ErrDeliveryState = errors.New("feature state does not accept delivery links")

// shaRe matches a bare git commit SHA (short to full form).
var shaRe = regexp.MustCompile(`^[0-9a-f]{7,40}$`)

const shaPlaceholder = "{sha}"

// ResolveDelivery validates the --commit/--mr input and resolves a bare SHA
// through the [delivery] commit_url template, so every stored delivery link
// is a full URL. It fails fast — callers run it before any write.
func ResolveDelivery(cfg project.Config, commit, mr string) (Delivery, error) {
	var d Delivery
	if mr != "" {
		if !isHTTPURL(mr) {
			return Delivery{}, fmt.Errorf("%w: --mr must be an http(s) URL, got %q", ErrDeliveryInvalid, mr)
		}
		d.MRURL = mr
	}
	switch {
	case commit == "":
	case isHTTPURL(commit):
		d.CommitURL = commit
	case shaRe.MatchString(commit):
		tmpl := cfg.Delivery.CommitURL
		if !strings.Contains(tmpl, shaPlaceholder) {
			return Delivery{}, fmt.Errorf("%w: pass a full commit URL or set [delivery] commit_url = \"https://…/%s\" in .cernerc", ErrDeliveryNoTemplate, shaPlaceholder)
		}
		d.CommitURL = strings.ReplaceAll(tmpl, shaPlaceholder, commit)
	default:
		return Delivery{}, fmt.Errorf("%w: --commit must be an http(s) URL or a 7-40 hex SHA, got %q", ErrDeliveryInvalid, commit)
	}
	return d, nil
}

// isHTTPURL reports whether s looks like an absolute http(s) URL.
func isHTTPURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

// SetFeatureDelivery is the post-hoc capture (F-0179): the definitive commit
// SHA often only exists after the squash-merge, past the complete. It updates
// the delivery fields on an in_development or implemented feature, appends a
// feature.delivered event and commits through the normal snapshot path —
// state and lifecycle documents untouched. Precedent for mutating implemented
// metadata: feature retag.
func SetFeatureDelivery(repoDir, projDir, id string, d Delivery) error {
	if d == (Delivery{}) {
		return fmt.Errorf("%w: nothing to record — pass --commit and/or --mr", ErrDeliveryInvalid)
	}
	return MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		f, err := store.LoadFeature(repoDir, id)
		if errors.Is(err, store.ErrNotFound) {
			return "", fmt.Errorf("feature %s not found in backend", id)
		}
		if err != nil {
			return "", err
		}
		if f.State != domain.StateInDevelopment && f.State != domain.StateImplemented && f.State != domain.StateDone {
			return "", fmt.Errorf("%w: %s is %s", ErrDeliveryState, id, f.State)
		}
		if d.CommitURL != "" {
			f.DeliveryCommitURL = d.CommitURL
		}
		if d.MRURL != "" {
			f.DeliveryMRURL = d.MRURL
		}
		f.UpdatedAt = time.Now().UTC()
		// Only an implemented feature's result carries the block: a draft gets it
		// at complete, and a done result is history, left untouched (F-0355).
		if f.State == domain.StateImplemented {
			if err := stampDeliveryInResult(repoDir, f); err != nil {
				return "", err
			}
		}
		if err := store.SaveFeature(repoDir, f); err != nil {
			return "", err
		}
		payload := map[string]string{}
		if d.CommitURL != "" {
			payload["commit_url"] = d.CommitURL
		}
		if d.MRURL != "" {
			payload["mr_url"] = d.MRURL
		}
		if err := store.AppendEvent(repoDir, domain.NewEvent(id, "feature.delivered", payload, EventActor())); err != nil {
			return "", err
		}
		return fmt.Sprintf("cerne: deliver %s", id), nil
	})
}

// stampDeliveryInResult writes f's "## Entrega" block into its result.md in the
// backend clone (F-0355). A feature with no delivery link leaves the result
// alone; the write joins the caller's critical section.
func stampDeliveryInResult(repoDir string, f store.Feature) error {
	if f.DeliveryCommitURL == "" && f.DeliveryMRURL == "" {
		return nil
	}
	path := FeatureDocPath(repoDir, f.ID, "result.md")
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil // no result to carry the block
	}
	if err != nil {
		return err
	}
	body := StampDelivery(string(raw), f.DeliveryCommitURL, f.DeliveryMRURL)
	if body == string(raw) {
		return nil
	}
	return os.WriteFile(path, []byte(body), 0o644)
}
