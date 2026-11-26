package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/room"
)

// runRoomIn is runRoom with stdin, for --minutes - (T-0022).
func runRoomIn(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()
	cmd := newRoomCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

// clearRoomCredEnv leaves a room command only the credentials a test passes:
// no inherited variable and no agent session store.
func clearRoomCredEnv(t *testing.T) {
	t.Helper()
	t.Setenv("CERNE_ROOM_SESSION", "")
	t.Setenv("CERNE_ROOM_FACILITATOR", "")
	t.Setenv("CERNE_SESSION", "")
}

// T-0022 crit. 1
// E-0006 crit. 10
func TestRoomMinutesDraftSavesFromEverySource(t *testing.T) {
	clearRoomCredEnv(t)
	dir, port := newRoomTestServer(t, "proj-minutes-draft")
	dirFlag := []string{"--dir", dir, "--port", strconv.Itoa(port)}
	_, facilitator := openRoomCLI(t, dirFlag, "topic")
	sess := joinRoomAs(t, dirFlag, "R1", "ana")

	t.Setenv("CERNE_ROOM_SESSION", sess)
	out, err := runRoom(t, append([]string{"minutes", "draft", "R1", "--minutes", `{"conclusion":"inline","rationale":"r"}`}, dirFlag...)...)
	if err != nil {
		t.Fatalf("draft inline: %v", err)
	}
	for _, want := range []string{"minutes draft saved in R1 by ana", "cerne room minutes show R1 --draft", "cerne room close R1 --use-draft"} {
		if !strings.Contains(out, want) {
			t.Fatalf("draft output = %q, want %q", out, want)
		}
	}
	path := filepath.Join(t.TempDir(), "ata.json")
	if err := os.WriteFile(path, []byte(`{"conclusion":"from file","rationale":"r"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := runRoom(t, append([]string{"minutes", "draft", "R1", "--minutes", path}, dirFlag...)...); err != nil {
		t.Fatalf("draft from file: %v", err)
	}
	if out, _ := runRoom(t, append([]string{"minutes", "show", "R1", "--draft"}, dirFlag...)...); !strings.Contains(out, "from file") {
		t.Fatalf("show --draft after the file = %q", out)
	}

	t.Setenv("CERNE_ROOM_SESSION", "")
	t.Setenv("CERNE_ROOM_FACILITATOR", facilitator)
	out, err = runRoomIn(t, "{\"conclusion\":\"from stdin\",\n\"rationale\":\"r\",\"owners\":[\"ana\"]}", append([]string{"minutes", "draft", "R1", "--minutes", "-"}, dirFlag...)...)
	if err != nil || !strings.Contains(out, "by facilitator") {
		t.Fatalf("draft from stdin as facilitator = %q, %v", out, err)
	}
	out, err = runRoom(t, append([]string{"minutes", "show", "R1", "--draft"}, dirFlag...)...)
	if err != nil {
		t.Fatalf("show --draft: %v", err)
	}
	for _, want := range []string{"# Rascunho de ata — R1 (topic)\n", "- Autor: facilitator\n", "- Atualizado em: ", "## Conclusão\n\nfrom stdin\n", "## Responsáveis\n\n- ana\n"} {
		if !strings.Contains(out, want) {
			t.Fatalf("show --draft = %q, want %q", out, want)
		}
	}
	if again, _ := runRoom(t, append([]string{"minutes", "show", "R1", "--draft"}, dirFlag...)...); again != out {
		t.Fatalf("show --draft is not deterministic:\n%s\nthen\n%s", out, again)
	}
}

// T-0022 crit. 1
// E-0006 crit. 10
func TestRoomMinutesDraftRefusals(t *testing.T) {
	clearRoomCredEnv(t)
	dir, port := newRoomTestServer(t, "proj-minutes-draft-refusals")
	dirFlag := []string{"--dir", dir, "--port", strconv.Itoa(port)}
	_, facilitator := openRoomCLI(t, dirFlag, "topic")
	sess := joinRoomAs(t, dirFlag, "R1", "ana")
	draft := func(extra ...string) error {
		_, err := runRoom(t, append(append([]string{"minutes", "draft", "R1"}, extra...), dirFlag...)...)
		return err
	}
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"no credential", []string{"--minutes", minimalMinutesJSON}, "CERNE_ROOM_FACILITATOR"},
		{"both credentials", []string{"--minutes", minimalMinutesJSON, "--session", sess, "--facilitator", facilitator}, "not both"},
		{"no minutes", []string{"--session", sess}, "--minutes"},
		{"invalid json", []string{"--session", sess, "--minutes", `{"conclusion":`}, "--minutes"},
		{"incomplete", []string{"--session", sess, "--minutes", `{"conclusion":"ok"}`}, "required field"},
		{"participant as facilitator", []string{"--facilitator", sess, "--minutes", minimalMinutesJSON}, "not the room facilitator"},
	} {
		if err := draft(tc.args...); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want it to mention %q", tc.name, err, tc.want)
		}
	}
	if _, err := runRoom(t, append([]string{"minutes", "show", "R1", "--draft"}, dirFlag...)...); err == nil || !strings.Contains(err.Error(), "has no minutes draft") {
		t.Fatalf("show --draft with no draft err = %v, want a refusal", err)
	}
	if _, err := runRoom(t, append([]string{"minutes", "draft", room.ChannelID, "--session", sess, "--minutes", minimalMinutesJSON}, dirFlag...)...); !errors.Is(err, room.ErrPermanentRoom) {
		t.Fatalf("draft on the channel err = %v, want ErrPermanentRoom", err)
	}
}

// T-0022 crit. 2
// E-0006 crit. 11
func TestRoomCloseUseDraft(t *testing.T) {
	clearRoomCredEnv(t)
	dir, port := newRoomTestServer(t, "proj-close-use-draft")
	dirFlag := []string{"--dir", dir, "--port", strconv.Itoa(port)}
	_, facilitator := openRoomCLI(t, dirFlag, "topic")
	sess := joinRoomAs(t, dirFlag, "R1", "ana")
	closeR1 := func(extra ...string) (string, error) {
		return runRoom(t, append(append([]string{"close", "R1"}, extra...), dirFlag...)...)
	}

	if _, err := closeR1("--facilitator", facilitator, "--use-draft", "--minutes", minimalMinutesJSON); err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("--use-draft with --minutes err = %v, want mutually exclusive", err)
	}
	if _, err := closeR1("--facilitator", facilitator); err == nil || !strings.Contains(err.Error(), "--use-draft") {
		t.Fatalf("close with neither err = %v, want it to offer --use-draft", err)
	}
	if _, err := closeR1("--facilitator", facilitator, "--use-draft"); err == nil || !strings.Contains(err.Error(), "no minutes draft") {
		t.Fatalf("close --use-draft with no draft err = %v", err)
	}
	if _, err := runRoom(t, append([]string{"minutes", "draft", "R1", "--session", sess, "--minutes", `{"conclusion":"reverter o deploy","rationale":"mais rapido"}`}, dirFlag...)...); err != nil {
		t.Fatalf("draft: %v", err)
	}
	if _, err := closeR1("--facilitator", sess, "--use-draft"); err == nil || !strings.Contains(err.Error(), "not the room facilitator") {
		t.Fatalf("close --use-draft by a participant err = %v, want the facilitator refusal", err)
	}
	out, err := closeR1("--facilitator", facilitator, "--use-draft")
	if err != nil || !strings.Contains(out, "room R1 closed with the minutes draft by ana, saved at 20") {
		t.Fatalf("close --use-draft = %q, %v; want the draft's author and time", out, err)
	}
	if out, _ := runRoom(t, append([]string{"minutes", "show", "R1"}, dirFlag...)...); !strings.Contains(out, "reverter o deploy") {
		t.Fatalf("minutes after close --use-draft = %q, want the draft's conclusion", out)
	}
	if _, err := runRoom(t, append([]string{"minutes", "show", "R1", "--draft"}, dirFlag...)...); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("show --draft on a closed room err = %v", err)
	}
}

// T-0022 crit. 2
// E-0006 crit. 11
func TestRoomCloseHintsOpenItemsOnlyForThatRefusal(t *testing.T) {
	clearRoomCredEnv(t)
	dir, port := newRoomTestServer(t, "proj-close-hint")
	dirFlag := []string{"--dir", dir, "--port", strconv.Itoa(port)}
	_, facilitator := openRoomCLI(t, dirFlag, "topic")
	sess := joinRoomAs(t, dirFlag, "R1", "ana")
	if _, err := runRoom(t, append([]string{"propose", "R1", "--session", sess, "--title", "usar postgres"}, dirFlag...)...); err != nil {
		t.Fatalf("propose: %v", err)
	}
	for _, extra := range [][]string{nil, {"--allow-open-items"}} {
		args := append([]string{"close", "R1", "--facilitator", facilitator, "--minutes", `{"conclusion":"","rationale":""}`}, extra...)
		out, err := runRoom(t, append(args, dirFlag...)...)
		if err == nil || !strings.Contains(err.Error(), "required field") {
			t.Fatalf("close %v with incomplete minutes err = %v, want its own refusal", extra, err)
		}
		if strings.Contains(out, "close refused") {
			t.Fatalf("close %v output = %q, want no open-items hint for another refusal", extra, out)
		}
	}
}
