package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/henriquesakata/cerne/internal/doclang"
	"github.com/henriquesakata/cerne/internal/domain"
)

var (
	// ErrAlreadyInitialized is the sentinel for an InitLayout call on a
	// backend that already has a project.json.
	ErrAlreadyInitialized = errors.New("backend already initialized")
	// ErrUnknownIDPrefix is the sentinel for a NextID prefix with no entity subdir.
	ErrUnknownIDPrefix = errors.New("unknown id prefix")
)

const (
	projectFilename = "project.json"
	eventsSubdir    = "events"
)

// layoutSubdirsInit is every directory InitLayout creates, slash-separated.
var layoutSubdirsInit = []string{
	featuresSubdir,
	decisionsSubdir,
	referencesSubdir,
	runbooksSubdir,
	notesSubdir,
	historicalsSubdir,
	worksSubdir,
	pendingsSubdir,
	ideasSubdir,
	handoffsSubdir,
	eventsSubdir,
	"content/features",
	"content/decisions",
	"content/references",
	"content/runbooks",
	"content/notes",
	"content/artifacts",
	"content/wiki",
	"content/handoffs",
}

// idSubdirs maps a NextID prefix to its entity subdir.
var idSubdirs = map[string]string{
	"F":  featuresSubdir,
	// E- (epic) and T- (task) reuse the feature entity (F-0203): same subdir,
	// per-prefix sequences (NextID counts only files matching its own prefix).
	"E":  featuresSubdir,
	"T":  featuresSubdir,
	"D":  decisionsSubdir,
	"R":  referencesSubdir,
	"RB": runbooksSubdir,
	"N":  notesSubdir,
	"H":  historicalsSubdir,
	"W":  worksSubdir,
	"C":  capabilitiesSubdir,
	"P":  pendingsSubdir,
	"I":  ideasSubdir,
	"HO": handoffsSubdir,
}

// Project is the persisted form of project.json.
type Project struct {
	Name    string   `json:"name"`
	Modules []string `json:"modules"`
	// DocsLanguage is the language cerne scaffolds this project's documents
	// in (F-0359): "en" or "pt-BR". Empty is pt-BR, what every backend was
	// before the field existed. It lives here, not in .cernerc, because it is
	// a property of the documents: every checkout and every host that serves
	// this backend must scaffold in the same language.
	DocsLanguage string `json:"docs_language,omitempty"`
}

// DocsLanguage returns the language to scaffold documents in for the backend
// at backendDir. A missing project.json, an empty field or a value outside
// the catalog all read as Portuguese: scaffolding never fails on it.
func DocsLanguage(backendDir string) doclang.Lang {
	p, err := LoadProject(backendDir)
	if err != nil {
		return doclang.PT
	}
	l, err := doclang.Parse(p.DocsLanguage)
	if err != nil {
		return doclang.PT
	}
	return l
}

// InitLayout creates the backend directory tree (each dir with an empty
// .gitkeep) and writes project.json. ErrAlreadyInitialized when project.json
// already exists.
func InitLayout(backendDir string, p Project) error {
	projectPath := filepath.Join(backendDir, projectFilename)
	if _, err := os.Stat(projectPath); err == nil {
		return fmt.Errorf("%w: %s", ErrAlreadyInitialized, projectPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for _, sub := range layoutSubdirsInit {
		dir := filepath.Join(backendDir, filepath.FromSlash(sub))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, ".gitkeep"), nil, 0o644); err != nil {
			return err
		}
	}
	return writeJSONFile(backendDir, projectFilename, p)
}

// LoadProject reads project.json; ErrNotFound wrapped when missing.
func LoadProject(backendDir string) (Project, error) {
	var p Project
	data, err := os.ReadFile(filepath.Join(backendDir, projectFilename))
	if errors.Is(err, os.ErrNotExist) {
		return p, fmt.Errorf("%w: %s", ErrNotFound, projectFilename)
	}
	if err != nil {
		return p, err
	}
	if err := json.Unmarshal(data, &p); err != nil {
		return p, err
	}
	return p, nil
}

// SaveProject rewrites project.json whole (the rename path, F-0331).
func SaveProject(backendDir string, p Project) error {
	return writeJSONFile(backendDir, projectFilename, p)
}

// AppendEvent appends the event as one NDJSON line to
// events/<YYYY-MM>.ndjson, partitioned by ev.At in UTC.
func AppendEvent(backendDir string, ev domain.Event) error {
	ev.Payload = stampOrigin(ev.Payload)
	line, err := ev.MarshalNDJSON()
	if err != nil {
		return err
	}
	dir := filepath.Join(backendDir, eventsSubdir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	name := ev.At.UTC().Format("2006-01") + ".ndjson"
	f, err := os.OpenFile(filepath.Join(dir, name), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// stampOrigin adds the chat-channel provenance (F-0323) to a payload when
// CERNE_ORIGIN is set. Only the chat verb executor (internal/verbs.Run) sets
// these variables, and only in its child's env, so every event a tool call
// writes carries origin/conversation/turn plus the call's nonce. Existing keys
// win; the caller's map is never mutated.
func stampOrigin(p map[string]string) map[string]string {
	origin := os.Getenv("CERNE_ORIGIN")
	if origin == "" {
		return p
	}
	out := make(map[string]string, len(p)+4)
	for k, v := range map[string]string{
		"origin":       origin,
		"conversation": os.Getenv("CERNE_CONVERSATION"),
		"turn":         os.Getenv("CERNE_TURN"),
		"tool_call":    os.Getenv("CERNE_TOOL_CALL"),
	} {
		out[k] = v
	}
	for k, v := range p {
		out[k] = v
	}
	return out
}

// ReadEvents is the funnel's log: every event except idea.*, which never
// reaches an aggregation (F-0312). ReadAllEvents is the raw log, for the chat
// channel's affected ids.
func ReadEvents(backendDir string) ([]domain.Event, error) {
	all, err := ReadAllEvents(backendDir)
	if err != nil {
		return nil, err
	}
	events := all[:0]
	for _, ev := range all {
		if !strings.HasPrefix(ev.Type, domain.IdeaEventPrefix) {
			events = append(events, ev)
		}
	}
	return events, nil
}

// ReadAllEvents returns all events from events/*.ndjson in file-name then
// line order; empty slice when the dir or files are absent.
func ReadAllEvents(backendDir string) ([]domain.Event, error) {
	dir := filepath.Join(backendDir, eventsSubdir)
	dirEntries, err := os.ReadDir(dir) // sorted by filename
	if errors.Is(err, os.ErrNotExist) {
		return []domain.Event{}, nil
	}
	if err != nil {
		return nil, err
	}
	events := []domain.Event{}
	for _, de := range dirEntries {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".ndjson") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, de.Name()))
		if err != nil {
			return nil, err
		}
		for _, line := range strings.Split(string(data), "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			ev, err := domain.UnmarshalNDJSON([]byte(line))
			if err != nil {
				return nil, fmt.Errorf("%s: %w", de.Name(), err)
			}
			events = append(events, ev)
		}
	}
	return events, nil
}

// NextID returns the next sequential ID for the prefix (a key of idSubdirs:
// "F", "D", "P", "I", …),
// zero-padded to 4 digits: max existing numeric suffix + 1, or 0001 when
// the entity dir is empty or absent.
func NextID(backendDir, prefix string) (string, error) {
	subdir, ok := idSubdirs[prefix]
	if !ok {
		return "", fmt.Errorf("%w: %q", ErrUnknownIDPrefix, prefix)
	}
	dirEntries, err := os.ReadDir(filepath.Join(backendDir, filepath.FromSlash(subdir)))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	maxNum := 0
	for _, de := range dirEntries {
		name := de.Name()
		if de.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		suffix, found := strings.CutPrefix(strings.TrimSuffix(name, ".json"), prefix+"-")
		if !found {
			continue
		}
		n, err := strconv.Atoi(suffix)
		if err != nil || n <= 0 {
			continue
		}
		if n > maxNum {
			maxNum = n
		}
	}
	if maxNum >= 9999 {
		return "", fmt.Errorf("id sequence exhausted for prefix %q", prefix)
	}
	return fmt.Sprintf("%s-%04d", prefix, maxNum+1), nil
}
