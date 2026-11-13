package app

import (
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/store"
)

func pinZone(t *testing.T, name string) {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Skipf("no tz database: %v", err)
	}
	old := logZone
	logZone = loc
	t.Cleanup(func() { logZone = old })
}

func TestLogTimeShowsTheReadersZone(t *testing.T) {
	pinZone(t, "America/Sao_Paulo")
	if got, want := LogTime("2026-11-13T15:33:00Z"), "12:33 -03 (15:33Z)"; got != want {
		t.Fatalf("Sao_Paulo: %q, want %q", got, want)
	}
	logZone = time.UTC
	if got, want := LogTime("2026-11-13T15:33:00Z"), "15:33 UTC (15:33Z)"; got != want {
		t.Fatalf("UTC: %q, want %q", got, want)
	}
}

func TestRenderWorkLog(t *testing.T) {
	pinZone(t, "America/Sao_Paulo")
	got := RenderWorkLog([]store.LogEntry{
		{Seq: 1, UTC: "2026-11-13T15:33:00Z", Actor: "owner", RecordedBy: "agent:a@h", Text: "Sim, mergeia"},
		{Seq: 2, UTC: "2026-11-13T15:40:00Z", Actor: "agent:a@h", RecordedBy: "agent:a@h", Text: "mergeado"},
		{Seq: 3, UTC: "2026-11-14T02:30:00Z", Actor: "owner", RecordedBy: "owner", Text: "era outro", Corrects: 1}, // 23:30 local on the 13th
		{Seq: 4, UTC: "2026-11-14T03:30:00Z", Actor: "owner", RecordedBy: "owner", Text: "dia seguinte"},           // 00:30 local on the 14th
	})
	want := strings.Join([]string{
		"2026-11-13",
		"#1  12:33 -03 (15:33Z)  owner (recorded by agent:a@h)  Sim, mergeia",
		"#2  12:40 -03 (15:40Z)  agent:a@h  mergeado",
		"#3  23:30 -03 (02:30Z)  owner  era outro (corrects #1)",
		"2026-11-14",
		"#4  00:30 -03 (03:30Z)  owner  dia seguinte",
	}, "\n")
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}
