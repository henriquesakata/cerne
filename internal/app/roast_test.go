package app

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// stubRoastProvider lets each test script the model per prompt and observe
// concurrency without a real LLM.
type stubRoastProvider struct {
	mu       sync.Mutex
	inFlight int
	maxSeen  int
	reply    func(ctx context.Context, prompt string) (string, error)
	delay    time.Duration
}

func (s *stubRoastProvider) Complete(ctx context.Context, prompt string) (string, error) {
	s.mu.Lock()
	s.inFlight++
	if s.inFlight > s.maxSeen {
		s.maxSeen = s.inFlight
	}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.inFlight--
		s.mu.Unlock()
	}()
	if s.delay > 0 {
		select {
		case <-time.After(s.delay):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return s.reply(ctx, prompt)
}

// TestBuildRoastPromptContract pins the critic contract for every persona
// (F-0271 criteria 1/4): single lens, exact-quote-or-invalid, no verdict,
// fresh context (only the final spec, explicitly delimited), capped findings.
func TestBuildRoastPromptContract(t *testing.T) {
	if len(RoastPersonas) != 3 {
		t.Fatalf("RoastPersonas = %d, want exactly 3", len(RoastPersonas))
	}
	for _, p := range RoastPersonas {
		prompt := BuildRoastPrompt(p, "F-0001", "Login social", "## Problema\n\nSenha fraca.")
		for _, want := range []string{
			"trecho EXATO",
			"nunca aprove nem reprove",
			"contexto fresco",
			"no máximo 5",
			"=== SPEC (início) ===",
			"=== SPEC (fim) ===",
			"Login social",
			"Senha fraca.",
			p.Lens,
		} {
			if !strings.Contains(prompt, want) {
				t.Fatalf("persona %s: prompt missing %q:\n%s", p.Key, want, prompt)
			}
		}
	}
	// Lenses must differ — three copies of the same critic is redundancy, not diversity.
	if RoastPersonas[0].Lens == RoastPersonas[1].Lens || RoastPersonas[1].Lens == RoastPersonas[2].Lens {
		t.Fatalf("personas share a lens: %+v", RoastPersonas)
	}
}

// TestRunRoastParallel: the three critics run concurrently and a full round
// aggregates one output per persona.
func TestRunRoastParallel(t *testing.T) {
	stub := &stubRoastProvider{
		delay: 50 * time.Millisecond,
		reply: func(context.Context, string) (string, error) { return "- [alta] achado — trecho: \"x\"", nil },
	}
	rep := RunRoast(context.Background(), stub, "F-0001", "t", "spec", time.Second)
	if rep.Partial {
		t.Fatalf("Partial = true on a clean round: %+v", rep)
	}
	if len(rep.Results) != 3 {
		t.Fatalf("Results = %d, want 3", len(rep.Results))
	}
	for _, r := range rep.Results {
		if r.Err != "" || !strings.Contains(r.Output, "[alta]") {
			t.Fatalf("result %s = %+v", r.Persona.Key, r)
		}
	}
	if stub.maxSeen < 2 {
		t.Fatalf("critics ran sequentially (max in-flight %d), want parallel", stub.maxSeen)
	}
}

// TestRunRoastPartial pins F-0271 criterion 1: a failing or timed-out critic
// becomes a marked partial without killing the round.
func TestRunRoastPartial(t *testing.T) {
	stub := &stubRoastProvider{
		reply: func(ctx context.Context, prompt string) (string, error) {
			if strings.Contains(prompt, RoastPersonas[1].Lens) {
				// honors ctx like the real providers (exec.CommandContext / HTTP)
				select {
				case <-time.After(300 * time.Millisecond):
					return "late", nil
				case <-ctx.Done():
					return "", ctx.Err()
				}
			}
			return "- [média] ok — trecho: \"y\"", nil
		},
	}
	rep := RunRoast(context.Background(), stub, "F-0001", "t", "spec", 50*time.Millisecond)
	if !rep.Partial {
		t.Fatalf("Partial = false, want true: %+v", rep)
	}
	var failed, fine int
	for _, r := range rep.Results {
		if r.Err != "" {
			failed++
		} else {
			fine++
		}
	}
	if failed != 1 || fine != 2 {
		t.Fatalf("failed=%d fine=%d, want 1/2: %+v", failed, fine, rep.Results)
	}
}

// TestFormatRoastReport: the persisted markdown carries every persona section
// and marks the partial one instead of hiding it.
func TestFormatRoastReport(t *testing.T) {
	rep := RoastReport{FeatureID: "F-0001", Title: "Login social", At: time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC), SpecSHA: "ab12cd34"}
	rep.Results = []RoastResult{
		{Persona: RoastPersonas[0], Output: "- [alta] a — trecho: \"x\""},
		{Persona: RoastPersonas[1], Err: "context deadline exceeded"},
		{Persona: RoastPersonas[2], Output: "sem achados relevantes"},
	}
	rep.Partial = true
	md := FormatRoastReport(rep)
	for _, want := range []string{
		"F-0001",
		"Rodada: 2026-08-14T12:00:00Z",
		"spec sha256:ab12cd34",
		RoastPersonas[0].Title,
		RoastPersonas[1].Title,
		RoastPersonas[2].Title,
		"- [alta] a",
		"parcial",
		"context deadline exceeded",
		"sem achados relevantes",
	} {
		if !strings.Contains(md, want) {
			t.Fatalf("report missing %q:\n%s", want, md)
		}
	}
}
