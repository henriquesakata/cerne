package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gitlab.com/rockt/cerne/internal/domain"
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
	historicalsSubdir,
	eventsSubdir,
	"content/features",
	"content/decisions",
	"content/references",
	"content/runbooks",
	"content/artifacts",
	"content/wiki",
}

// idSubdirs maps a NextID prefix to its entity subdir.
var idSubdirs = map[string]string{
	"F":  featuresSubdir,
	"D":  decisionsSubdir,
	"R":  referencesSubdir,
	"RB": runbooksSubdir,
	"H":  historicalsSubdir,
}

// Project is the persisted form of project.json.
type Project struct {
	Name    string   `json:"name"`
	Modules []string `json:"modules"`
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

// AppendEvent appends the event as one NDJSON line to
// events/<YYYY-MM>.ndjson, partitioned by ev.At in UTC.
func AppendEvent(backendDir string, ev domain.Event) error {
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

// ReadEvents returns all events from events/*.ndjson in file-name then
// line order; empty slice when the dir or files are absent.
func ReadEvents(backendDir string) ([]domain.Event, error) {
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

// NextID returns the next sequential ID for the prefix ("F", "D" or "R"),
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
