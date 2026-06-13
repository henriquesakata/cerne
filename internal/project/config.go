// Package project handles the per-project files cerne keeps in the user's
// repository (.cernerc, docs.lock) and the local cache layout.
package project

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// ErrNoProject is the sentinel for a LoadConfig walk that reaches the
// filesystem root without finding a .cernerc.
var ErrNoProject = errors.New("no cerne project")

const configFilename = ".cernerc"

// Config is the persisted form of .cernerc (TOML, committed by the user).
type Config struct {
	ProjectID    string    `toml:"project_id"`    // slug, e.g. "cerne"
	Backend      string    `toml:"backend"`       // git URL or local path
	DefaultScope string    `toml:"default_scope"` // optional module filter
	LLM          LLMConfig `toml:"llm"`           // optional v1.5 LLM layer config
}

// LLMConfig is the optional [llm] block of .cernerc. A secret never lives
// here — a future BYO-key mode reads its key from the environment.
type LLMConfig struct {
	Mode string `toml:"mode"` // "delegation" (default when empty); "byo-key" reserved
}

// SaveConfig writes <dir>/.cernerc as TOML.
func SaveConfig(dir string, c Config) error {
	data, err := toml.Marshal(c)
	if err != nil {
		return err
	}
	return writeFileAtomic(dir, configFilename, data)
}

// LoadConfig walks up from startDir until a .cernerc is found and returns
// the decoded config plus the directory containing it. ErrNoProject when no
// .cernerc exists up to the filesystem root.
func LoadConfig(startDir string) (Config, string, error) {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return Config{}, "", err
	}
	for {
		path := filepath.Join(dir, configFilename)
		data, err := os.ReadFile(path)
		if err == nil {
			var c Config
			if err := toml.Unmarshal(data, &c); err != nil {
				return Config{}, "", fmt.Errorf("%s: %w", path, err)
			}
			return c, dir, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return Config{}, "", err
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return Config{}, "", fmt.Errorf("%w: no %s from %s up to filesystem root", ErrNoProject, configFilename, startDir)
		}
		dir = parent
	}
}

// writeFileAtomic writes data to <dir>/<filename> atomically: temp file in
// the same dir, then rename.
func writeFileAtomic(dir, filename string, data []byte) error {
	tmp, err := os.CreateTemp(dir, filename+".*.tmp")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), filepath.Join(dir, filename)); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}
