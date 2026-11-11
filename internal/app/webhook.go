package app

import (
	"fmt"
	"os"
	"time"

	"github.com/henriquesakata/cerne/internal/notify"
	"github.com/henriquesakata/cerne/internal/project"
)

// dispatchWebhook reports a transition via the configured [webhook] (F-0206).
// Called after the transition's commit+push already succeeded (crit. 5: a
// delivered webhook never reports a transition that didn't persist) — never
// inside the backend lock's critical section, so a slow/dead endpoint can
// never delay it. A missing/unreadable .cernerc or an unset webhook URL is a
// silent no-op (crit. 4); any Send failure is a stderr warning only — it
// never surfaces as an error to the caller (crit. 2).
func dispatchWebhook(projDir, id, kind, action, from, to, title string) {
	cfg, _, err := project.LoadConfig(projDir)
	if err != nil || cfg.Webhook.URL == "" {
		return
	}
	ev := notify.Event{
		ID:     id,
		Kind:   kind,
		Action: action,
		From:   from,
		To:     to,
		Actor:  EventActor(),
		Title:  title,
		At:     time.Now().UTC(),
	}
	if err := notify.Send(cfg.Webhook, ev); err != nil {
		fmt.Fprintf(os.Stderr, "warning: webhook: %v\n", err)
	}
}
