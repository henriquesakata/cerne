package server

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"gitlab.com/rockt/cerne/internal/gitback"
)

// headPollInterval is how often the watcher reads the backend clone's HEAD.
// One ref read per second per running `cerne ui` is negligible.
const headPollInterval = time.Second

// keepaliveInterval bounds idle time on an SSE connection so proxies and the
// browser keep it open between real change events.
const keepaliveInterval = 25 * time.Second

// liveHub is a tiny Server-Sent-Events broadcaster: it fans out a single
// "the backend advanced" signal (the new HEAD sha) to every connected
// GET /api/stream subscriber. The signal is intentionally minimal — clients
// refetch the view they are on, so the server stays the one source of truth.
type liveHub struct {
	mu   sync.Mutex
	subs map[chan string]struct{}
}

func newLiveHub() *liveHub {
	return &liveHub{subs: make(map[chan string]struct{})}
}

// subscribe registers a new subscriber and returns its channel (buffer 1: a
// pending signal is all a client needs — it refetches the latest state anyway).
func (h *liveHub) subscribe() chan string {
	ch := make(chan string, 1)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

// unsubscribe removes ch and closes it. Safe to call more than once: a channel
// no longer in the set is left untouched.
func (h *liveHub) unsubscribe(ch chan string) {
	h.mu.Lock()
	if _, ok := h.subs[ch]; ok {
		delete(h.subs, ch)
		close(ch)
	}
	h.mu.Unlock()
}

// broadcast delivers sha to every subscriber without blocking: a full buffer
// means a signal is already pending for that client, which is enough.
func (h *liveHub) broadcast(sha string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- sha:
		default:
		}
	}
}

// watchHead polls the backend clone's HEAD and broadcasts whenever it advances
// — any local commit (lifecycle move, doc edit, artifact, reference) or a pull
// that brought remote commits. It returns when ctx is cancelled. A read error
// (e.g. an unborn HEAD mid-clone) is skipped, not fatal.
func (h *liveHub) watchHead(ctx context.Context, repoDir string, interval time.Duration) {
	last, _ := gitback.HeadSHA(repoDir) // "" on unborn; the first real sha broadcasts
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sha, err := gitback.HeadSHA(repoDir)
			if err != nil {
				continue
			}
			if sha != last {
				last = sha
				h.broadcast(sha)
			}
		}
	}
}

// pullLoop fast-forwards the backend clone from origin every interval so a
// teammate's push reaches the dashboard. It never talks to the hub: a pull that
// brings commits advances HEAD, which watchHead then notices — one notification
// path for local and remote changes alike. Pull errors (offline, auth) are
// ignored so the dashboard keeps serving the cached clone.
func pullLoop(ctx context.Context, repoDir string, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = gitback.Pull(repoDir)
		}
	}
}

// handleStream is the SSE endpoint (GET /api/stream). It emits a "change" event
// carrying the new backend HEAD sha whenever the backend advances, so the UI
// refetches without a manual reload. It sends the current sha on connect (so a
// reconnecting client resyncs and never misses a change across a brief drop),
// keeps the connection warm with comment lines, and returns — unsubscribing —
// when the client disconnects (request context cancelled).
func (h *liveHub) handleStream(w http.ResponseWriter, r *http.Request, repoDir string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, errorBody("streaming não suportado"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := h.subscribe()
	defer h.unsubscribe(ch)

	// Resync on connect: emit the current sha right away.
	if sha, err := gitback.HeadSHA(repoDir); err == nil {
		fmt.Fprintf(w, "event: change\ndata: %s\n\n", sha)
		flusher.Flush()
	}

	keepalive := time.NewTicker(keepaliveInterval)
	defer keepalive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case sha, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "event: change\ndata: %s\n\n", sha)
			flusher.Flush()
		case <-keepalive.C:
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}
