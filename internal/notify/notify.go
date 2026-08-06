// Package notify sends a best-effort HTTP webhook on every entity state
// transition (F-0206). It never fails the caller: Send returns an error for
// the caller to log as a warning, never to propagate.
package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"gitlab.com/rockt/cerne/internal/project"
)

// Event is the payload of a single transition (crit. 1): {id, kind, action,
// from, to, actor, title, at}. Kind is the entity kind ("feature" covers
// both feature and epic — same entity, role inferred; "decision"; "work").
type Event struct {
	ID     string    `json:"id"`
	Kind   string    `json:"kind"`
	Action string    `json:"action"`
	From   string    `json:"from"`
	To     string    `json:"to"`
	Actor  string    `json:"actor"`
	Title  string    `json:"title"`
	At     time.Time `json:"at"`
}

const defaultTimeout = 3 * time.Second

// Send POSTs ev to cfg.URL per cfg.Format. A no-op when cfg.URL is empty
// (crit. 4: no config, no request). format = "slack" sends a Slack incoming
// webhook payload ({"text": …}, crit. 3); anything else (default "json")
// sends ev itself (crit. 1). A non-2xx response or a request that outlives
// cfg.Timeout (default 3s) returns an error — the caller decides how to warn.
func Send(cfg project.WebhookConfig, ev Event) error {
	if cfg.URL == "" {
		return nil
	}
	var payload any = ev
	if cfg.Format == "slack" {
		payload = map[string]string{
			"text": fmt.Sprintf("%s %s: %s → %s (%s)", ev.ID, ev.Title, ev.From, ev.To, ev.Actor),
		}
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, cfg.URL, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	timeout := defaultTimeout
	if cfg.Timeout > 0 {
		timeout = time.Duration(cfg.Timeout) * time.Second
	}
	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook %s: status %d", cfg.URL, resp.StatusCode)
	}
	return nil
}
