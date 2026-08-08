package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"

	"gitlab.com/rockt/cerne/internal/project"
	"gitlab.com/rockt/cerne/internal/room"
)

// roomClient is a thin HTTP client for the war room API (T-0001) exposed by
// a running `cerne ui`. The pid is resolved locally from .cernerc
// (resolvePaths, no network) — the room routes are chatty and don't warrant
// the remote pull that resolveProject would trigger.
type roomClient struct {
	base string // e.g. http://127.0.0.1:7717/api/projects/<pid>
	port int
}

func newRoomClient(dir string, port int) (roomClient, error) {
	if dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return roomClient{}, err
		}
		dir = wd
	}
	// LoadConfig only reads the committed .cernerc — unlike resolveProject/
	// resolvePaths it never touches the cached backend clone (which may not
	// even exist yet), so a chatty room command never pays a clone/pull.
	cfg, _, err := project.LoadConfig(dir)
	if err != nil {
		return roomClient{}, err
	}
	return roomClient{
		base: fmt.Sprintf("http://127.0.0.1:%d/api/projects/%s", port, url.PathEscape(cfg.ProjectID)),
		port: port,
	}, nil
}

// do performs an HTTP call against the war room API and decodes the JSON
// response into out (nil to discard the body). A connection failure or a 404
// (server up but this project isn't registered with it) both mean the same
// thing to the caller: no usable `cerne ui` for this project. ctx lets a
// `read --follow` loop (each call may block up to the server's long-poll
// timeout) be cut short by Ctrl+C between iterations.
func (c roomClient) do(ctx context.Context, method, path string, headers map[string]string, body, out any) error {
	var reqBody *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reqBody = bytes.NewReader(b)
	} else {
		reqBody = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, reqBody)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("no cerne ui answering for this project on port %d (start one with 'cerne ui'): %w", c.port, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&e)
		if e.Error != "" && e.Error != "route not found" {
			return fmt.Errorf("no cerne ui running this project on port %d (start one with 'cerne ui'): %s", c.port, e.Error)
		}
		return fmt.Errorf("no cerne ui answering for this project on port %d (start one with 'cerne ui')", c.port)
	}
	if resp.StatusCode >= 300 {
		var e struct {
			Error string `json:"error"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&e); err == nil && e.Error != "" {
			return fmt.Errorf("%s", e.Error)
		}
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// roomOpenResult mirrors the POST /rooms response (F-0228): the public room
// snapshot plus the private facilitator capability, minted once for the
// opener.
type roomOpenResult struct {
	Room          room.Room `json:"room"`
	FacilitatorID string    `json:"facilitator_id"`
}

func (c roomClient) open(ctx context.Context, topic string) (roomOpenResult, error) {
	var res roomOpenResult
	err := c.do(ctx, http.MethodPost, "/rooms", nil, map[string]string{"topic": topic}, &res)
	return res, err
}

func (c roomClient) list(ctx context.Context) ([]room.Room, error) {
	var out struct {
		Rooms []room.Room `json:"rooms"`
	}
	err := c.do(ctx, http.MethodGet, "/rooms", nil, nil, &out)
	return out.Rooms, err
}

// roomJoinResult mirrors the POST .../join response (F-0226): the public room
// snapshot plus the private session capability minted for this participant.
type roomJoinResult struct {
	Room    room.Room    `json:"room"`
	Session room.Session `json:"session"`
}

func (c roomClient) join(ctx context.Context, id, nick, bio string) (roomJoinResult, error) {
	var res roomJoinResult
	err := c.do(ctx, http.MethodPost, "/rooms/"+url.PathEscape(id)+"/join", nil, map[string]string{"nick": nick, "bio": bio}, &res)
	return res, err
}

func (c roomClient) say(ctx context.Context, id, sessionID, body string) (room.Message, error) {
	var msg room.Message
	err := c.do(ctx, http.MethodPost, "/rooms/"+url.PathEscape(id)+"/messages", nil, map[string]string{"session_id": sessionID, "body": body}, &msg)
	return msg, err
}

func (c roomClient) read(ctx context.Context, id string, since int, follow bool) ([]room.Message, error) {
	q := fmt.Sprintf("?since=%d", since)
	if follow {
		q += "&follow=1"
	}
	var out struct {
		Messages []room.Message `json:"messages"`
	}
	err := c.do(ctx, http.MethodGet, "/rooms/"+url.PathEscape(id)+"/messages"+q, nil, nil, &out)
	return out.Messages, err
}

func (c roomClient) close(ctx context.Context, id, facilitatorID string, minutes room.Minutes, allowOpenItems bool) (room.Room, error) {
	var rm room.Room
	body := map[string]any{"facilitator_id": facilitatorID, "minutes": minutes, "allow_open_items": allowOpenItems}
	err := c.do(ctx, http.MethodPost, "/rooms/"+url.PathEscape(id)+"/close", nil, body, &rm)
	return rm, err
}

// handoffFacilitator rotates the room's facilitator capability (F-0228
// criterion 1), returning the new one; current is invalidated in the same
// step.
func (c roomClient) handoffFacilitator(ctx context.Context, id, facilitatorID string) (string, error) {
	var out struct {
		FacilitatorID string `json:"facilitator_id"`
	}
	err := c.do(ctx, http.MethodPost, "/rooms/"+url.PathEscape(id)+"/facilitator/handoff", nil, map[string]string{"facilitator_id": facilitatorID}, &out)
	return out.FacilitatorID, err
}

// createItem raises a proposal or decision-request item as sessionID's
// participant (F-0228 criterion 2). kind is "proposal" or "decision_request".
func (c roomClient) createItem(ctx context.Context, id, sessionID, kind, title, body string) (room.FacilitationItem, error) {
	var item room.FacilitationItem
	req := map[string]string{"session_id": sessionID, "kind": kind, "title": title, "body": body}
	err := c.do(ctx, http.MethodPost, "/rooms/"+url.PathEscape(id)+"/items", nil, req, &item)
	return item, err
}

// pending lists id's open facilitation items (F-0228 criterion 3).
func (c roomClient) pending(ctx context.Context, id, facilitatorID string) ([]room.FacilitationItem, error) {
	var out struct {
		Items []room.FacilitationItem `json:"items"`
	}
	headers := map[string]string{"X-Room-Facilitator": facilitatorID}
	err := c.do(ctx, http.MethodGet, "/rooms/"+url.PathEscape(id)+"/items/pending", headers, nil, &out)
	return out.Items, err
}

// resolve resolves an open facilitation item; only the facilitator may
// (F-0228 criterion 4).
func (c roomClient) resolve(ctx context.Context, id, facilitatorID, itemID, result string) (room.FacilitationItem, error) {
	var item room.FacilitationItem
	req := map[string]string{"facilitator_id": facilitatorID, "result": result}
	err := c.do(ctx, http.MethodPost, "/rooms/"+url.PathEscape(id)+"/items/"+url.PathEscape(itemID)+"/resolve", nil, req, &item)
	return item, err
}
