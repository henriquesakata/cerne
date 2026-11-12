package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/doccheck"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/lint"
	"github.com/henriquesakata/cerne/internal/store"
)

// ErrUnknownIDPrefix is returned by `cerne show` when a cerne_id's prefix maps
// to no known entity kind (F/D/R/N/RB/H/C/P/I).
var ErrUnknownIDPrefix = errors.New("unknown cerne_id prefix")

// ErrNoSuchDoc is returned by `cerne show --doc` when the requested document
// name is not one of a feature's slots, the entity is not a feature, or the
// slot holds no real content yet.
var ErrNoSuchDoc = errors.New("no such document")

// featureDocNames are the document slots a feature carries, in reading order.
var featureDocNames = []string{"spec", "plan", "result"}

// showResult is one resolved cerne_id: its kind, the loaded entity value, and
// any markdown content sections read straight from the backend store. Feature
// docs split into spec/plan/result; other kinds carry a single body (name "").
// pendingShow is how show presents a pending (F-0885): the stored entity plus
// the state of the feature it is promoted to, flat in the JSON as
// promoted_to_state.
type pendingShow struct {
	store.Pending
	PromotedToState domain.FeatureState `json:"promoted_to_state,omitempty"`
}

type showResult struct {
	kind    string
	entity  any
	content []showDoc
	// approvalStale is set for features only (F-0318): whether the approval
	// went stale (lint.SpecChangedAfterApproval).
	approvalStale *bool
	// amendments are a feature's spec/plan amends, newest first (F-0338).
	amendments []amendmentJSON
}

// amendmentJSON is one amend of a feature's spec or plan: the revision that
// keeps the replaced text, when, by whom and why (the feature.doc_edited event).
type amendmentJSON struct {
	Doc    string    `json:"doc"`
	Rev    int       `json:"rev"`
	At     time.Time `json:"at"`
	Actor  string    `json:"actor"`
	Reason string    `json:"reason"`
}

// addAmendments reads the amend events of feature id (feature.doc_edited with
// a rev) newest first and, when there is any, adds them with the
// `## amendments` block and the hint to read a revision.
func (r *showResult) addAmendments(repoDir, id string) error {
	evs, err := store.ReadEvents(repoDir)
	if err != nil {
		return err
	}
	var lines []string
	for i := len(evs) - 1; i >= 0; i-- {
		ev := evs[i]
		rev, err := strconv.Atoi(ev.Payload["rev"])
		if ev.EntityID != id || ev.Type != "feature.doc_edited" || err != nil {
			continue
		}
		a := amendmentJSON{ev.Payload["doc"], rev, ev.At, ev.Actor, ev.Payload["reason"]}
		r.amendments = append(r.amendments, a)
		lines = append(lines, fmt.Sprintf("%s r%d · %s · %s · %s", a.Doc, a.Rev, a.At.Format(time.DateOnly), a.Actor, a.Reason))
	}
	if len(lines) > 0 {
		lines = append(lines, "read one: cerne show "+id+" --doc <doc> --rev N")
		r.content = append(r.content, showDoc{name: "amendments", body: strings.Join(lines, "\n")})
	}
	return nil
}

type showDoc struct {
	name string // "spec"/"plan"/"result" for features; "" for single-body kinds
	body string
}

func newShowCmd() *cobra.Command {
	var dir, doc string
	var asJSON, consolidated, all bool
	var rev int
	cmd := &cobra.Command{
		Use:   "show <cerne_id>",
		Short: "Show any entity (feature, decision, reference, note, runbook, historical, pending, idea) by id",
		Long: "Resolves a cerne_id by its prefix (F-/D-/R-/N-/RB-/H-/P-/I-) and prints the metadata plus the entity's " +
			"markdown body, read straight from the backend (store) — works for any entity, including " +
			"already-implemented features that are not materialized in '.docs/'. A feature prints the document " +
			"of its stage (spec; plan in development; result once implemented) and names the others: --doc " +
			"prints a single feature document raw (spec|plan|result), --all prints every one, and --json is " +
			"machine output with every document.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			revSet := cmd.Flags().Changed("rev")
			if kind, _ := kindForID(strings.ToUpper(strings.TrimSpace(args[0]))); revSet && kind == "feature" {
				if all {
					return errors.New("--rev prints one document revision: --all does not apply")
				}
				if consolidated || asJSON {
					return errors.New("--rev prints a feature document revision as text: --consolidado and --json do not apply")
				}
				return runShowDocRevision(cmd, dir, args[0], doc, rev)
			}
			if consolidated || rev != 0 {
				if consolidated && rev != 0 {
					return errors.New("--consolidado and --rev are alternatives: pick one")
				}
				if asJSON || doc != "" {
					return errors.New("--consolidado and --rev print the runbook body as text: --json and --doc do not apply")
				}
				return runShowRunbookView(cmd, dir, args[0], consolidated, rev)
			}
			return runShow(cmd, dir, args[0], doc, asJSON, all)
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "feature only: print every document (spec, plan, result), not just the one of its stage")
	cmd.Flags().BoolVar(&consolidated, "consolidado", false, "runbook only: the body with each erratum placed at the step it changes (read-only)")
	cmd.Flags().IntVar(&rev, "rev", 0, "print revision N: of a runbook's body (the one before each errata merge stays readable), or with --doc spec|plan of a feature document (the one before each amend)")
	cmd.Flags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.Flags().MarkHidden("dir")
	cmd.Flags().StringVar(&doc, "doc", "", "print only this feature document, raw: "+strings.Join(featureDocNames, "|"))
	cmd.Flags().BoolVar(&asJSON, "json", false, "machine-readable JSON output (entity + content)")
	return cmd
}

// runShowDocRevision prints revision rev of a feature's spec or plan raw, byte
// for byte (F-0338): the text before each amend, or the current one at K+1.
func runShowDocRevision(cmd *cobra.Command, dir, id, doc string, rev int) error {
	if doc != "spec" && doc != "plan" {
		return fmt.Errorf("%w: --rev on a feature reads a spec or plan revision: pass --doc spec or --doc plan", ErrNoSuchDoc)
	}
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	id = strings.ToUpper(strings.TrimSpace(id))
	if _, err := store.LoadFeature(ctx.repoDir, id); err != nil {
		return fmt.Errorf("feature %s: %w", id, err)
	}
	k, err := store.CountDocRevisions(ctx.repoDir, id, doc)
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(ctx.repoDir, "content", "features", id, doc+".md")); k == 0 && errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%s has no %s and no revisions of it", id, doc)
	}
	if rev < 1 {
		return fmt.Errorf("%s %s has revisions 1..%d, no revision %d", id, doc, k+1, rev)
	}
	body, err := store.LoadDocRevision(ctx.repoDir, id, doc, rev)
	if err != nil {
		return err
	}
	fmt.Fprint(cmd.OutOrStdout(), body)
	return nil
}

// runShow prints id. Without all, a feature's text is narrowed to the document
// of its stage (stageView); --doc and --json are never narrowed.
func runShow(cmd *cobra.Command, dir, id, doc string, asJSON, all bool) error {
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	id = strings.ToUpper(strings.TrimSpace(id)) // ids are stored upper-case: i-0001 is I-0001
	res, err := resolveEntity(ctx.repoDir, id)
	if err != nil {
		return err
	}
	if res.kind == "feature" {
		// F-0338: the amends of the spec and plan, after the documents and before
		// the errata — the lines, never the replaced bodies (read with --rev).
		if err := res.addAmendments(ctx.repoDir, id); err != nil {
			return err
		}
	}
	er, err := store.LoadErrata(ctx.repoDir, id)
	if err != nil {
		return err
	}
	if body := strings.TrimSpace(string(stripLeadingFrontmatter([]byte(er)))); body != "" {
		res.content = append(res.content, showDoc{name: "errata", body: body})
	}
	// F-0345: a runbook's errata items not yet folded into the body, and those
	// with no step; nil while nothing is open.
	var openErrata, steplessErrata *int
	if res.kind == "runbook" {
		var open, stepless int
		for _, it := range store.ParseErrataItems(er) {
			if it.Open() {
				open++
				if it.Target == "" {
					stepless++
				}
			}
		}
		if open > 0 {
			openErrata, steplessErrata = &open, &stepless
		}
	}
	// F-0339 crit. 9: a work's session handoffs, after its fields.
	var handoffs []store.HandoffEntry
	if res.kind == "work" {
		if handoffs, err = store.LoadHandoffs(ctx.repoDir, id); err != nil {
			return err
		}
		var parts []string
		for _, h := range handoffs {
			parts = append(parts, h.String())
		}
		if len(parts) > 0 {
			res.content = append(res.content, showDoc{name: "handoff", body: strings.Join(parts, "\n\n")})
		}
	}
	if f, ok := res.entity.(store.Feature); ok {
		stale, _, _ := lint.SpecChangedAfterApproval(ctx.repoDir, f)
		res.approvalStale = &stale
	}
	if res.kind == "feature" {
		// Backlink (F-0322): the delivery lists the debts it left open.
		pendings, err := store.LoadAllPendings(ctx.repoDir)
		if err != nil {
			return err
		}
		var lines, pays []string
		for _, p := range pendings {
			if p.From == id && p.State == domain.PendingOpen {
				lines = append(lines, p.ID+"  "+p.Title)
			}
			// F-0885: and the debts it pays, in whatever state each stands.
			if p.PromotedTo == id {
				pays = append(pays, fmt.Sprintf("%s  [%s]  %s", p.ID, p.State, p.Title))
			}
		}
		if len(lines) > 0 {
			res.content = append(res.content, showDoc{name: "pending", body: strings.Join(lines, "\n")})
		}
		if len(pays) > 0 {
			res.content = append(res.content, showDoc{name: "pays", body: strings.Join(pays, "\n")})
		}
	}
	if p, ok := res.entity.(store.Pending); ok {
		// F-0885: the forward link says how the feature that pays it stands.
		ps := pendingShow{Pending: p}
		if p.PromotedTo != "" {
			if f, err := store.LoadFeature(ctx.repoDir, p.PromotedTo); err == nil {
				ps.PromotedToState = f.State
			}
		}
		res.entity = ps
	}
	if res.kind == "idea" {
		// C20: the idea's own trail, from the raw log — idea.* never reaches
		// the funnel's ReadEvents (History, heatmap).
		evs, err := store.ReadAllEvents(ctx.repoDir)
		if err != nil {
			return err
		}
		var lines []string
		for _, ev := range evs {
			if ev.EntityID != id {
				continue
			}
			line := fmt.Sprintf("%s  %s  %s", ev.At.Format(time.RFC3339), ev.Type, ev.Actor)
			for _, k := range slices.Sorted(maps.Keys(ev.Payload)) {
				line += fmt.Sprintf("  %s=%s", k, ev.Payload[k])
			}
			lines = append(lines, line)
		}
		if len(lines) > 0 {
			res.content = append(res.content, showDoc{name: "history", body: strings.Join(lines, "\n")})
		}
	}
	w := cmd.OutOrStdout()
	if doc != "" {
		if err := res.selectDoc(doc); err != nil {
			return fmt.Errorf("%s --doc %s: %w", id, doc, err)
		}
		if !asJSON {
			fmt.Fprintf(w, "%s\n", res.content[0].body)
			return nil
		}
	}
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		j := res.toJSON(handoffs)
		j.OpenErrata, j.SteplessErrata = openErrata, steplessErrata
		return enc.Encode(j)
	}
	if f, ok := res.entity.(store.Feature); ok && !all {
		res.stageView(f.State, id)
	}
	if openErrata != nil { // say where the version that applies is
		fmt.Fprintf(w, "%d open errata item(s) (%d without a step). What applies now: cerne show %s --consolidado\n", *openErrata, *steplessErrata, id)
	}
	res.renderText(w)
	return nil
}

// stageView narrows a feature's documents to the one of its stage (F-0338):
// the most advanced real document up to the state's — the plan in
// development, the result once implemented or done, the spec otherwise. The
// blocks stay; the documents left out close the show as one line, with their
// sizes and how to read them.
func (r *showResult) stageView(state domain.FeatureState, id string) {
	upTo := 0 // spec
	switch state {
	case domain.StateInDevelopment:
		upTo = 1
	case domain.StateImplemented, domain.StateDone:
		upTo = 2
	}
	pick := ""
	for _, d := range r.content { // the documents come in featureDocNames order
		if i := slices.Index(featureDocNames, d.name); i >= 0 && i <= upTo {
			pick = d.name
		}
	}
	var kept []showDoc
	var others []string
	for _, d := range r.content {
		if d.name != pick && slices.Contains(featureDocNames, d.name) {
			others = append(others, fmt.Sprintf("%s (%d lines)", d.name, strings.Count(d.body, "\n")+1))
			continue
		}
		kept = append(kept, d)
	}
	if len(others) > 0 {
		kept = append(kept, showDoc{"", "other documents: " + strings.Join(others, " · ") + " — cerne show " + id + " --doc <doc> | --all"})
	}
	r.content = kept
}

// selectDoc narrows the result to the single named feature document, failing
// when the name is not a feature slot, the entity is not a feature, or the slot
// has no real content.
func (r *showResult) selectDoc(name string) error {
	if r.kind != "feature" {
		article := "a"
		if strings.ContainsRune("aeiou", rune(r.kind[0])) {
			article = "an"
		}
		return fmt.Errorf("%w: only features have spec/plan/result documents (this is %s %s)", ErrNoSuchDoc, article, r.kind)
	}
	if !slices.Contains(featureDocNames, name) {
		return fmt.Errorf("%w: valid: %s", ErrNoSuchDoc, strings.Join(featureDocNames, ", "))
	}
	for _, d := range r.content {
		if d.name == name {
			r.content = []showDoc{d}
			return nil
		}
	}
	return fmt.Errorf("%w: not written yet", ErrNoSuchDoc)
}

// kindForID maps a cerne_id to its entity kind by the prefix before the first
// dash. RB is matched as a whole token (the dash split makes it unambiguous
// against R).
func kindForID(id string) (string, bool) {
	i := strings.IndexByte(id, '-')
	if i <= 0 {
		return "", false
	}
	switch id[:i] {
	case "F", "T", "E":
		return "feature", true
	case "D":
		return "decision", true
	case "R":
		return "reference", true
	case "N":
		return "note", true
	case "RB":
		return "runbook", true
	case "H":
		return "historical", true
	case "C":
		return "capability", true
	case "P":
		return "pending", true
	case "I":
		return "idea", true
	case "W":
		return "work", true
	}
	return "", false
}

// resolveEntity loads the entity for id and gathers its content. History is
// included by construction: the loaders read every entity regardless of state.
func resolveEntity(repoDir, id string) (showResult, error) {
	kind, ok := kindForID(id)
	if !ok {
		return showResult{}, fmt.Errorf("%q: %w", id, ErrUnknownIDPrefix)
	}
	switch kind {
	case "feature":
		f, err := store.LoadFeature(repoDir, id)
		if err != nil {
			return showResult{}, fmt.Errorf("feature %s: %w", id, err)
		}
		return showResult{kind, f, featureContent(repoDir, id), nil, nil}, nil
	case "decision":
		d, err := store.LoadDecision(repoDir, id)
		if err != nil {
			return showResult{}, fmt.Errorf("decision %s: %w", id, err)
		}
		return showResult{kind, d, singleContent(repoDir, "decisions", id), nil, nil}, nil
	case "reference":
		r, err := store.LoadReference(repoDir, id)
		if err != nil {
			return showResult{}, fmt.Errorf("reference %s: %w", id, err)
		}
		return showResult{kind, r, singleContent(repoDir, "references", id), nil, nil}, nil
	case "note":
		n, err := store.LoadNote(repoDir, id)
		if err != nil {
			return showResult{}, fmt.Errorf("note %s: %w", id, err)
		}
		return showResult{kind, n, singleContent(repoDir, "notes", id), nil, nil}, nil
	case "runbook":
		rb, err := store.LoadRunbook(repoDir, id)
		if err != nil {
			return showResult{}, fmt.Errorf("runbook %s: %w", id, err)
		}
		return showResult{kind, rb, singleContent(repoDir, "runbooks", id), nil, nil}, nil
	case "historical":
		h, err := store.LoadHistorical(repoDir, id)
		if err != nil {
			return showResult{}, fmt.Errorf("historical %s: %w", id, err)
		}
		return showResult{kind, h, nil, nil, nil}, nil
	case "capability":
		c, err := store.LoadCapability(repoDir, id)
		if err != nil {
			return showResult{}, fmt.Errorf("capability %s: %w", id, err)
		}
		return showResult{kind, c, nil, nil, nil}, nil
	case "pending":
		p, err := store.LoadPending(repoDir, id)
		if err != nil {
			return showResult{}, fmt.Errorf("pending %s: %w", id, err)
		}
		var content []showDoc
		if p.Body != "" {
			content = []showDoc{{"", p.Body}}
		}
		return showResult{kind, p, content, nil, nil}, nil
	case "work":
		wk, err := store.LoadWork(repoDir, id)
		if err != nil {
			return showResult{}, fmt.Errorf("work %s: %w", id, err)
		}
		// F-0873: a work that records a runbook execution carries its run.
		var content []showDoc
		if run, err := store.LoadRun(repoDir, id); err == nil {
			content = []showDoc{{"run", renderRun(run)}}
		}
		return showResult{kind, wk, content, nil, nil}, nil
	case "idea":
		if _, err := app.CanonicalIdeaID(id); err != nil {
			return showResult{}, err
		}
		i, err := store.LoadIdea(repoDir, id)
		if err != nil {
			return showResult{}, fmt.Errorf("idea %s: %w", id, err)
		}
		return showResult{kind, i, []showDoc{{"", i.Text}}, nil, nil}, nil
	}
	return showResult{}, fmt.Errorf("%q: %w", id, ErrUnknownIDPrefix)
}

// featureContent reads the spec/plan/result docs, skipping any that are still
// stub scaffolds or absent.
func featureContent(repoDir, id string) []showDoc {
	var docs []showDoc
	for _, name := range featureDocNames {
		body := readRealDoc(filepath.Join(repoDir, "content", "features", id, name+".md"))
		if body != "" {
			docs = append(docs, showDoc{name, body})
		}
	}
	return docs
}

// singleContent reads the single markdown body of a non-feature entity stored at
// content/<subdir>/<id>.md, skipping a stub or missing file.
func singleContent(repoDir, subdir, id string) []showDoc {
	body := readRealDoc(filepath.Join(repoDir, "content", subdir, id+".md"))
	if body == "" {
		return nil
	}
	return []showDoc{{"", body}}
}

// readRealDoc returns the trimmed markdown body of a content doc, or "" when the
// file is missing or is still a stub scaffold (no real content yet).
func readRealDoc(path string) string {
	data, err := os.ReadFile(path)
	if err != nil || !doccheck.HasRealContent(data) {
		return ""
	}
	return strings.TrimRight(string(data), "\n")
}

func (r showResult) renderText(w io.Writer) {
	switch e := r.entity.(type) {
	case store.Feature:
		fmt.Fprintf(w, "%s  %s  [%s]  %s\n", e.ID, r.kind, e.State, e.Title)
		if e.Module != "" {
			fmt.Fprintf(w, "module: %s\n", e.Module)
		}
		if e.Parent != "" {
			fmt.Fprintf(w, "epic: %s\n", e.Parent)
		}
		if e.FromIdea != "" {
			fmt.Fprintf(w, "from idea: %s\n", e.FromIdea)
		}
		if r.approvalStale != nil && *r.approvalStale {
			fmt.Fprintln(w, approvalStaleLine(e.ID))
		}
		if e.DeliveryCommitURL != "" {
			fmt.Fprintf(w, "delivery commit: %s\n", e.DeliveryCommitURL)
		}
		if e.DeliveryMRURL != "" {
			fmt.Fprintf(w, "delivery mr: %s\n", e.DeliveryMRURL)
		}
		writeSuccession(w, e.SupersededBy, e.Reason)
	case store.Decision:
		fmt.Fprintf(w, "%s  %s  [%s]  %s\n", e.ID, r.kind, e.State, e.Title)
		writeSuccession(w, e.SupersededBy, e.Reason)
	case store.Reference:
		fmt.Fprintf(w, "%s  %s  [%s]  %s\n", e.ID, r.kind, e.State, e.Title)
		if e.URL != "" {
			fmt.Fprintf(w, "url: %s\n", e.URL)
		}
		if e.External {
			fmt.Fprintln(w, "external: yes")
		}
		if len(e.Informs) > 0 {
			fmt.Fprintf(w, "informs: %s\n", strings.Join(e.Informs, ", "))
		}
	case store.Note:
		fmt.Fprintf(w, "%s  %s  [%s]  %s\n", e.ID, r.kind, e.State, e.Title)
		if e.Durable {
			fmt.Fprintln(w, "durable: yes")
		}
		if len(e.Informs) > 0 {
			fmt.Fprintf(w, "informs: %s\n", strings.Join(e.Informs, ", "))
		}
		writeSuccession(w, e.SupersededBy, e.Reason)
	case store.Runbook:
		fmt.Fprintf(w, "%s  %s  [%s]  %s\n", e.ID, r.kind, e.State, e.Title)
		writeSuccession(w, e.SupersededBy, e.Reason)
	case store.Historical:
		fmt.Fprintf(w, "%s  %s  [%s]  %s\n", e.ID, r.kind, e.State, e.Name)
		if len(e.Aliases) > 0 {
			fmt.Fprintf(w, "aliases: %s\n", strings.Join(e.Aliases, ", "))
		}
		writeSuccession(w, e.SupersededBy, e.Reason)
	case store.Capability:
		fmt.Fprintf(w, "%s  %s  [%s]  %s\n", e.ID, r.kind, e.State, e.Title)
		if e.Parent != "" {
			fmt.Fprintf(w, "parent: %s\n", e.Parent)
		}
		if e.Reason != "" {
			fmt.Fprintf(w, "reason: %s\n", e.Reason)
		}
	case pendingShow:
		fmt.Fprintf(w, "%s  %s  [%s]  %s\n", e.ID, r.kind, e.State, e.Title)
		fmt.Fprintf(w, "from: %s\n", e.From)
		fmt.Fprintf(w, "created: %s\n", e.CreatedAt.Format(time.DateOnly))
		if e.ClosedAt != nil {
			fmt.Fprintf(w, "closed: %s\n", e.ClosedAt.Format(time.DateOnly))
		}
		if len(e.Facets) > 0 {
			fmt.Fprintf(w, "facets: %s\n", strings.Join(e.Facets, ", "))
		}
		if len(e.Components) > 0 {
			fmt.Fprintf(w, "components: %s\n", strings.Join(e.Components, ", "))
		}
		if e.PromotedTo != "" && e.PromotedToState != "" {
			fmt.Fprintf(w, "promoted to: %s (%s)\n", e.PromotedTo, e.PromotedToState)
		} else if e.PromotedTo != "" {
			fmt.Fprintf(w, "promoted to: %s\n", e.PromotedTo)
		}
		if e.Reason != "" {
			fmt.Fprintf(w, "reason: %s\n", e.Reason)
		}
	case store.Work:
		fmt.Fprintf(w, "%s  %s  [%s]  %s\n", e.ID, r.kind, e.State, e.Title)
		fmt.Fprintf(w, "actor: %s\n", e.Actor)
		if len(e.Refs) > 0 {
			fmt.Fprintf(w, "refs: %s\n", joinRefs(e.Refs))
		}
		if e.Note != "" {
			fmt.Fprintf(w, "note: %s\n", e.Note)
		}
	case store.Idea:
		fmt.Fprintf(w, "%s  %s  [%s]\n", e.ID, r.kind, e.State)
		fmt.Fprintf(w, "created: %s\n", e.CreatedAt.Format(time.DateOnly))
		if e.ClosedAt != nil {
			fmt.Fprintf(w, "closed: %s\n", e.ClosedAt.Format(time.DateOnly))
		}
		if len(e.Facets) > 0 {
			fmt.Fprintf(w, "facets: %s\n", strings.Join(e.Facets, ", "))
		}
		if len(e.Components) > 0 {
			fmt.Fprintf(w, "components: %s\n", strings.Join(e.Components, ", "))
		}
		if e.PromotedTo != "" {
			fmt.Fprintf(w, "promoted to: %s\n", e.PromotedTo)
		}
		if e.Reason != "" {
			fmt.Fprintf(w, "reason: %s\n", e.Reason)
		}
	}
	for _, d := range r.content {
		if d.name != "" {
			fmt.Fprintf(w, "\n## %s\n%s\n", d.name, d.body)
		} else {
			fmt.Fprintf(w, "\n%s\n", d.body)
		}
	}
}

// writeSuccession prints the shared "superseded by" / "reason" tail used by
// every entity that carries it.
func writeSuccession(w io.Writer, supersededBy, reason string) {
	if supersededBy != "" {
		fmt.Fprintf(w, "superseded by: %s\n", supersededBy)
	}
	if reason != "" {
		fmt.Fprintf(w, "reason: %s\n", reason)
	}
}

type showJSON struct {
	Kind           string               `json:"kind"`
	Entity         any                  `json:"entity"`
	Content        []showDocJSON        `json:"content,omitempty"`
	ApprovalStale  *bool                `json:"approval_stale,omitempty"`  // features only (F-0318)
	Amendments     []amendmentJSON      `json:"amendments,omitempty"`      // features only (F-0338)
	OpenErrata     *int                 `json:"open_errata,omitempty"`     // runbooks with open items only (F-0345)
	SteplessErrata *int                 `json:"stepless_errata,omitempty"` // idem: those with no step
	Handoffs       []store.HandoffEntry `json:"handoffs,omitempty"`        // works only (F-0339)
}

type showDocJSON struct {
	Name string `json:"name,omitempty"`
	Body string `json:"body"`
}

func (r showResult) toJSON(handoffs []store.HandoffEntry) showJSON {
	var docs []showDocJSON
	for _, d := range r.content {
		if d.name != "amendments" && d.name != "handoff" { // structured in Amendments / Handoffs
			docs = append(docs, showDocJSON{d.name, d.body})
		}
	}
	return showJSON{Kind: r.kind, Entity: r.entity, Content: docs, ApprovalStale: r.approvalStale, Amendments: r.amendments, Handoffs: handoffs}
}

// approvalStaleLine is how show and next flag a stale approval (F-0318): the
// reason and the way out, on one line.
func approvalStaleLine(id string) string {
	return "approval: stale — spec changed since approval (re-approve: cerne feature approve " + id + ")"
}
