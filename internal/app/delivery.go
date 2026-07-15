package app

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"gitlab.com/rockt/cerne/internal/project"
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
