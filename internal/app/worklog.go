package app

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// Refusals of AppendWorkLog (F-0397). None of them writes anything.
var (
	ErrLogEmpty        = errors.New("work log text is empty")
	ErrLogWorkNotFound = errors.New("work not found")
	ErrLogWorkClosed   = errors.New("work is not active")
	ErrLogPersonal     = errors.New("no e-mail or person id in the work log")
	ErrLogNoSuchEntry  = errors.New("work log entry does not exist")
)

// AppendWorkLog records one decision on active work: one new file in the
// backend, one commit. actor "" means the session identity (the one of `work
// start`); "owner" or a nickname is declared by the writer and never replaces
// RecordedBy, which is always the session identity. corrects > 0 points at an
// earlier entry. The number is taken inside the mutate, so a push that is
// rebased onto a newer tip recomputes it (F-0356 re-mint).
func AppendWorkLog(repoDir, projDir, work, actor, text string, corrects int, now time.Time) (store.LogEntry, error) {
	text, actor = strings.TrimSpace(text), strings.TrimSpace(actor)
	if text == "" {
		return store.LogEntry{}, ErrLogEmpty
	}
	if err := refusePersonal("--actor", actor); err != nil {
		return store.LogEntry{}, fmt.Errorf("%w: %v", ErrLogPersonal, err)
	}
	if err := refusePersonal("the text", text); err != nil {
		return store.LogEntry{}, fmt.Errorf("%w: %v", ErrLogPersonal, err)
	}
	work = strings.ToUpper(strings.TrimSpace(work))
	by := recorder()
	if actor == "" {
		actor = by
	}
	var e store.LogEntry
	err := MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		w, err := store.LoadWork(repoDir, work)
		if errors.Is(err, store.ErrNotFound) {
			return "", fmt.Errorf("%w: %s", ErrLogWorkNotFound, work)
		}
		if err != nil {
			return "", err
		}
		if w.State != domain.WorkActive {
			return "", fmt.Errorf("%w: %s is %s", ErrLogWorkClosed, work, w.State)
		}
		seq, err := store.NextLogSeq(repoDir, work)
		if err != nil {
			return "", err
		}
		if corrects != 0 {
			if corrects < 0 || corrects >= seq {
				return "", fmt.Errorf("%w: %s has no #%d", ErrLogNoSuchEntry, work, corrects)
			}
		}
		e = store.LogEntry{Seq: seq, UTC: now.UTC().Format(time.RFC3339), Actor: actor, RecordedBy: by, Text: text, Corrects: corrects}
		if err := store.SaveLogEntry(repoDir, work, e); err != nil {
			return "", err
		}
		return fmt.Sprintf("cerne: log #%d on %s", seq, work), nil
	})
	if err != nil {
		return store.LogEntry{}, err
	}
	return e, nil
}

// recorder is what a log entry stores as recorded_by (F-0407): the agent's
// shortened session, "agent", or "human" — never user@host, which has the
// shape of the e-mail the command itself refuses. Another explicit
// CERNE_ACTOR is kept only when refusePersonal lets it through.
func recorder() string {
	r := rawRecorder()
	if refusePersonal("--actor", r) != nil {
		if isAgentActor(r) {
			return legacyAgentActor
		}
		return "human"
	}
	return r
}

var sessionHexRe = regexp.MustCompile(`^[0-9a-f]{8}`)

func rawRecorder() string {
	switch a := actorEnv(); a {
	case "":
		return "human"
	case legacyAgentActor:
		if s := sessionHexRe.FindString(CurrentSession()); s != "" { // F-0525: also recognizes CURSOR_CONVERSATION_ID
			return agentActorPrefix + "session-" + s
		}
		return a
	default:
		return a
	}
}

// logZone is where entry times are shown: the reader's zone, never a fixed
// one. A variable so tests can pin it (time.Local does not reload on a TZ
// change at run time).
var logZone = time.Local

// LogTime shows a stored UTC time as "15:04 MST (15:04Z)" in the reader's zone.
// A time that does not parse is returned as stored.
func LogTime(utc string) string {
	t, err := time.Parse(time.RFC3339, utc)
	if err != nil {
		return utc
	}
	return t.In(logZone).Format("15:04 ") + zoneLabel(t.In(logZone)) + " (" + t.UTC().Format("15:04") + "Z)"
}

// brazilAbbr names the Brazilian zones by their usual abbreviation, which the
// tzdata dropped in 2017 (it now says -03). Keyed by the zone's UTC offset in
// hours; only used when the zone is Brazilian and its abbreviation is numeric.
var brazilAbbr = map[int]string{-2: "FNT", -3: "BRT", -4: "AMT", -5: "ACT"}

// zoneLabel is t's zone abbreviation, with a numeric one of a Brazilian zone
// (F-0407) replaced by its usual name. Other zones are shown as the tzdata has them.
func zoneLabel(t time.Time) string {
	abbr, off := t.Zone()
	if abbr == "" || (abbr[0] != '+' && abbr[0] != '-') || off%3600 != 0 {
		return abbr
	}
	name := logZone.String()
	if name == "Local" {
		name = os.Getenv("TZ")
		if name == "" {
			if l, err := os.Readlink("/etc/localtime"); err == nil {
				name = l
			}
		}
	}
	if !strings.Contains(name, "America/") {
		return abbr
	}
	if l, ok := brazilAbbr[off/3600]; ok && brazilZone(name) {
		return l
	}
	return abbr
}

// brazilZone reports whether a tz name (or a path ending in one) is Brazilian.
func brazilZone(name string) bool {
	for _, z := range brazilZones {
		if strings.HasSuffix(name, z) {
			return true
		}
	}
	return false
}

var brazilZones = []string{
	"Noronha", "Sao_Paulo", "Bahia", "Fortaleza", "Recife", "Belem", "Maceio",
	"Araguaina", "Santarem", "Campo_Grande", "Cuiaba", "Porto_Velho", "Boa_Vista",
	"Manaus", "Eirunepe", "Rio_Branco",
}

// RenderWorkLog lists entries in order, one per line, with the local date on a
// line of its own when the day changes.
func RenderWorkLog(entries []store.LogEntry) string {
	var lines []string
	day := ""
	for _, e := range entries {
		if t, err := time.Parse(time.RFC3339, e.UTC); err == nil {
			if d := t.In(logZone).Format(time.DateOnly); d != day {
				day = d
				lines = append(lines, d)
			}
		}
		who := e.Actor
		if e.RecordedBy != e.Actor {
			who += " (recorded by " + e.RecordedBy + ")"
		}
		line := fmt.Sprintf("#%d  %s  %s  %s", e.Seq, LogTime(e.UTC), who, e.Text)
		if e.Corrects > 0 {
			line += fmt.Sprintf(" (corrects #%d)", e.Corrects)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}
