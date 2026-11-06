// Package lastused remembers, for each folder that admits several profiles,
// the profile last started there.
package lastused

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type record struct {
	Profile string    `json:"profile"`
	At      time.Time `json:"at"`
}

// Path is the state file, ~/.local/state/ai-profile/last-used.json.
func Path(home string) string {
	return filepath.Join(home, ".local", "state", "ai-profile", "last-used.json")
}

func read(home string) map[string]record {
	m := map[string]record{}
	if b, err := os.ReadFile(Path(home)); err == nil {
		if json.Unmarshal(b, &m) != nil {
			return map[string]record{}
		}
	}
	return m
}

// Get returns the profile last started in dir, "" when none is recorded.
func Get(home, dir string) string { return read(home)[dir].Profile }

// Set records profile as the last started in dir.
func Set(home, dir, profile string) error {
	m := read(home)
	if m[dir].Profile == profile {
		return nil
	}
	m[dir] = record{Profile: profile, At: time.Now().UTC()}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	p := Path(home)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".last-used-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), p)
}
