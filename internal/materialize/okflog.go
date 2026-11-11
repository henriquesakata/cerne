package materialize

import (
	"sort"
	"strings"

	"github.com/henriquesakata/cerne/internal/domain"
)

// logMaxEntries caps the root log.md at the most recent events. A count (not a
// time window) keeps the projection deterministic: with no new events, two
// pulls at different times emit the same log. Fixed by design — no config.
const logMaxEntries = 200

// writeLog projects the event log as the OKF reserved log.md at the bundle
// root (F-0166): entries grouped by UTC date, newest first, each rendered as
// `* **<type>**: <entity> — <payload k=v, key-sorted> (<actor>)`. The entity
// id becomes a relative link when it was materialized in this pull (idPath),
// plain text otherwise — OKF consumers tolerate either.
func writeLog(docsDir string, events []domain.Event, idPath map[string]string, manifest map[string]string) error {
	sorted := make([]domain.Event, len(events))
	copy(sorted, events)
	sort.SliceStable(sorted, func(i, j int) bool {
		if !sorted[i].At.Equal(sorted[j].At) {
			return sorted[i].At.After(sorted[j].At)
		}
		return sorted[i].ID > sorted[j].ID
	})
	if len(sorted) > logMaxEntries {
		sorted = sorted[:logMaxEntries]
	}

	var b strings.Builder
	b.WriteString("# Update log\n")
	day := ""
	for _, ev := range sorted {
		if d := ev.At.UTC().Format("2006-01-02"); d != day {
			day = d
			b.WriteString("\n## " + d + "\n\n")
		}
		entity := ev.EntityID
		if rel, ok := idPath[ev.EntityID]; ok {
			entity = "[" + ev.EntityID + "](" + rel + ")"
		}
		b.WriteString("* **" + ev.Type + "**: " + entity)
		if len(ev.Payload) > 0 {
			keys := make([]string, 0, len(ev.Payload))
			for k := range ev.Payload {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			pairs := make([]string, len(keys))
			for i, k := range keys {
				// Multi-line payload values (e.g. notes) must not break the bullet.
				pairs[i] = k + "=" + strings.ReplaceAll(ev.Payload[k], "\n", " ")
			}
			b.WriteString(" — " + strings.Join(pairs, ", "))
		}
		if ev.Actor != "" {
			b.WriteString(" (" + ev.Actor + ")")
		}
		b.WriteString("\n")
	}

	sum, err := writeFile(docsDir, "log.md", []byte(b.String()))
	if err != nil {
		return err
	}
	manifest["log.md"] = sum
	return nil
}
