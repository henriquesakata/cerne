package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/index"
	"github.com/henriquesakata/cerne/internal/store"
)

// E-0005 verification 11 (criteria 15 and 16): the package with and without
// ring (d), and the binding index, on synthetic backends of 1,000 and 5,000
// entities shaped like this project's (most features done, an eighth notes,
// an eighth references), with bodies the size of real documents and the
// search index built. The peak memory is TestContextPeakMemory's, in a child
// process measured by /usr/bin/time.

// seedContextScale writes n entities under <tmp>/repo, builds the index next
// to it and returns the repo; the task is F-0001, live, with a plan citing
// paths and notes, sharing components with notes.
func seedContextScale(tb testing.TB, n int) string {
	tb.Helper()
	repo := filepath.Join(tb.TempDir(), "repo")
	now := time.Now().UTC()
	write := func(rel, body string) {
		p := filepath.Join(repo, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			tb.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			tb.Fatal(err)
		}
	}
	prose := strings.Repeat("The agent reads the task, the plan and the rules that inform it before the first edit. ", 30) // ~2.6 KB
	notes, refs, decs, rbs := n/8, n/8, n/80, n/500
	feats := n - notes - refs - decs - rbs
	live := n / 20
	for i := 1; i <= feats; i++ {
		id := fmt.Sprintf("F-%04d", i)
		st := domain.StateDone
		if i <= live {
			st = domain.StateInDevelopment
		}
		f := store.Feature{ID: id, Title: fmt.Sprintf("Feature %d about the package of a task", i), State: st,
			Components: []string{fmt.Sprintf("comp%d", i%150), fmt.Sprintf("comp%d", (i+1)%150)}, CreatedAt: now, UpdatedAt: now}
		if err := store.SaveFeature(repo, f); err != nil {
			tb.Fatal(err)
		}
		cites := ""
		for k := 0; k < 5; k++ {
			cites += fmt.Sprintf(" [[N-%04d]] `internal/pkg%d/file%d.go`", 1+(i*7+k)%notes, (i+k)%30, k)
		}
		write("content/features/"+id+"/spec.md", "# "+f.Title+"\n\n## Problem\n\nA task starts without the notes that inform it."+cites+"\n\n"+prose+"\n")
		write("content/features/"+id+"/plan.md", "# Plan\n\n"+cites+"\n\n"+prose+"\n")
		if st == domain.StateDone {
			write("content/features/"+id+"/result.md", "# Result\n\n"+prose+"\n")
		}
	}
	for i := 1; i <= notes; i++ {
		id := fmt.Sprintf("N-%04d", i)
		nt := store.Note{ID: id, Title: fmt.Sprintf("Rule %d of the index", i), State: domain.NoteActive, Gist: "do the thing, not the other",
			Components: []string{fmt.Sprintf("comp%d", i%150), fmt.Sprintf("area%d", i%40)}, CreatedAt: now, UpdatedAt: now}
		if i%8 == 0 {
			nt.When = []string{fmt.Sprintf("edit:internal/pkg%d/**", i%30)}
		}
		if i%3 == 0 {
			nt.Informs = []string{fmt.Sprintf("F-%04d", 1+i%live)}
		}
		if err := store.SaveNote(repo, nt); err != nil {
			tb.Fatal(err)
		}
		write("content/notes/"+id+".md", fmt.Sprintf("# %s\n\nBefore editing `internal/pkg%d/file%d.go`, mind the index. %s\n", nt.Title, i%30, i%10, prose[:600]))
	}
	for i := 1; i <= refs; i++ {
		id := fmt.Sprintf("R-%04d", i)
		if err := store.SaveReference(repo, store.Reference{ID: id, Title: fmt.Sprintf("Source %d", i), State: domain.ReferenceCurrent,
			Informs: []string{fmt.Sprintf("N-%04d", 1+i%notes)}, Components: []string{fmt.Sprintf("comp%d", i%150)}, CreatedAt: now, UpdatedAt: now}); err != nil {
			tb.Fatal(err)
		}
		write("content/references/"+id+".md", "# Source\n\n"+prose[:800]+"\n")
	}
	for i := 1; i <= decs; i++ {
		id := fmt.Sprintf("D-%04d", i)
		// Each decision informs a live feature, two of them F-0001 (T-0018).
		if err := store.SaveDecision(repo, store.Decision{ID: id, Title: fmt.Sprintf("Decision %d", i), State: domain.DecisionActive,
			Informs: []string{fmt.Sprintf("F-%04d", 1+(i-1)%max(1, decs/2))}, Components: []string{fmt.Sprintf("comp%d", i%150)}, CreatedAt: now, UpdatedAt: now}); err != nil {
			tb.Fatal(err)
		}
		write("content/decisions/"+id+".md", "# Decision\n\n"+prose+"\n")
	}
	for i := 1; i <= rbs; i++ {
		id := fmt.Sprintf("RB-%04d", i)
		if err := store.SaveRunbook(repo, store.Runbook{ID: id, Title: fmt.Sprintf("Runbook %d", i), State: domain.RunbookActive,
			Components: []string{fmt.Sprintf("comp%d", i%150)}, CreatedAt: now, UpdatedAt: now}); err != nil {
			tb.Fatal(err)
		}
	}
	if err := index.Rebuild(repo, filepath.Join(filepath.Dir(repo), "index.db"), time.Time{}); err != nil {
		tb.Fatal(err)
	}
	return repo
}

// benchPack times the package of F-0001, whose ring (a) holds the five notes
// its spec cites and the two decisions that inform it, all with their body.
func benchPack(b *testing.B, n int, content bool) {
	repo := seedContextScale(b, n)
	for b.Loop() {
		p, err := taskContext(repo, "F-0001", DefaultContextBudget, content)
		if err != nil || len(p.Items) == 0 {
			b.Fatalf("package: %v, %d items", err, len(p.Items))
		}
		if bodies := packBodies(p); bodies != 7 || p.Compact {
			b.Fatalf("package: %d bodies, compact %v; want the 7 linked bodies in full", bodies, p.Compact)
		}
	}
}

func packBodies(p TaskPack) int {
	n := 0
	for _, it := range p.Items {
		if it.Body != "" {
			n++
		}
	}
	return n
}

func benchIndex(b *testing.B, n int) {
	repo := seedContextScale(b, n)
	for b.Loop() {
		if items, err := TaskIndex(repo, "F-0001"); err != nil || len(items) == 0 {
			b.Fatalf("index: %v, %d items", err, len(items))
		}
	}
}

func BenchmarkTaskContext1000(b *testing.B)          { benchPack(b, 1000, true) }
func BenchmarkTaskContext1000NoContent(b *testing.B) { benchPack(b, 1000, false) }
func BenchmarkTaskContext5000(b *testing.B)          { benchPack(b, 5000, true) }
func BenchmarkTaskContext5000NoContent(b *testing.B) { benchPack(b, 5000, false) }
func BenchmarkTaskIndex1000(b *testing.B)            { benchIndex(b, 1000) }
func BenchmarkTaskIndex5000(b *testing.B)            { benchIndex(b, 5000) }

// maxRSSRe is the peak resident set in bytes that macOS /usr/bin/time -l
// prints.
var maxRSSRe = regexp.MustCompile(`(\d+)\s+maximum resident set size`)

// TestContextPeakMemory (criterion 16): the peak memory of the package and
// of the binding index on 5,000 entities, each run in a child process of
// this test binary under /usr/bin/time -l, below 64 MB. Opt-in (seeding
// takes seconds): CERNE_CONTEXT_MEM=1, macOS only.
func TestContextPeakMemory(t *testing.T) {
	if os.Getenv("CERNE_CONTEXT_MEM") == "" || runtime.GOOS != "darwin" {
		t.Skip("set CERNE_CONTEXT_MEM=1 on macOS to measure the peak memory")
	}
	repo := seedContextScale(t, 5000)
	load, _ := exec.Command("sysctl", "-n", "vm.loadavg").Output()
	for _, mode := range []string{"pack", "pack-no-content", "index"} {
		cmd := exec.Command("/usr/bin/time", "-l", os.Args[0], "-test.run=^TestContextMemChild$", "-test.count=1")
		cmd.Env = append(os.Environ(), "CERNE_CONTEXT_CHILD="+mode, "CERNE_CONTEXT_REPO="+repo)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %v\n%s", mode, err, out)
		}
		m := maxRSSRe.FindSubmatch(out)
		if m == nil {
			t.Fatalf("%s: no maximum resident set size in:\n%s", mode, out)
		}
		rss, _ := strconv.Atoi(string(m[1]))
		t.Logf("%s, 5000 entities: peak RSS %.1f MB (load average %s)", mode, float64(rss)/(1<<20), strings.TrimSpace(string(load)))
		if rss >= 64<<20 {
			t.Errorf("%s: peak RSS %d bytes, at or past 64 MB", mode, rss)
		}
	}
}

// TestContextMemChild is the child of TestContextPeakMemory: one package or
// one index, then exit.
func TestContextMemChild(t *testing.T) {
	mode, repo := os.Getenv("CERNE_CONTEXT_CHILD"), os.Getenv("CERNE_CONTEXT_REPO")
	if mode == "" {
		t.Skip("child of TestContextPeakMemory")
	}
	var err error
	switch mode {
	case "pack":
		_, err = taskContext(repo, "F-0001", DefaultContextBudget, true)
	case "pack-no-content":
		_, err = taskContext(repo, "F-0001", DefaultContextBudget, false)
	default:
		_, err = TaskIndex(repo, "F-0001")
	}
	if err != nil {
		t.Fatal(err)
	}
}
