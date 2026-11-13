package app

import (
	"errors"
	"fmt"
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
	by := resolveActor(defaultWorkActor())
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
		if corrects > 0 {
			if corrects >= seq {
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
	return t.In(logZone).Format("15:04 MST") + " (" + t.UTC().Format("15:04") + "Z)"
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
