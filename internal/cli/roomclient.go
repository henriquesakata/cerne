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
func (c roomClient) do(ctx context.Context, method, path string, body, out any) error {
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

func (c roomClient) open(ctx context.Context, topic string) (room.Room, error) {
	var rm room.Room
	err := c.do(ctx, http.MethodPost, "/rooms", map[string]string{"topic": topic}, &rm)
	return rm, err
}

func (c roomClient) list(ctx context.Context) ([]room.Room, error) {
	var out struct {
		Rooms []room.Room `json:"rooms"`
	}
	err := c.do(ctx, http.MethodGet, "/rooms", nil, &out)
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
	err := c.do(ctx, http.MethodPost, "/rooms/"+url.PathEscape(id)+"/join", map[string]string{"nick": nick, "bio": bio}, &res)
	return res, err
}

func (c roomClient) say(ctx context.Context, id, sessionID, body string) (room.Message, error) {
	var msg room.Message
	err := c.do(ctx, http.MethodPost, "/rooms/"+url.PathEscape(id)+"/messages", map[string]string{"session_id": sessionID, "body": body}, &msg)
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
	err := c.do(ctx, http.MethodGet, "/rooms/"+url.PathEscape(id)+"/messages"+q, nil, &out)
	return out.Messages, err
}

func (c roomClient) close(ctx context.Context, id string) (room.Room, error) {
	var rm room.Room
	err := c.do(ctx, http.MethodPost, "/rooms/"+url.PathEscape(id)+"/close", nil, &rm)
	return rm, err
}
