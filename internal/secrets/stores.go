package secrets

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/LunetteOrg/ai-profiles/internal/private"
)

// Stores is how ai-secrets reaches each password manager, from
// ~/.config/ai-secrets/config.toml. The defaults are the safe ones: every
// 1Password read gets its own approval, Bitwarden is locked after each read.
type Stores struct {
	Path       string
	TTL        time.Duration
	OpIsolated bool // every op read in a session of its own
	Bitwarden  map[string]*Bitwarden
	// Private names the file with the sensitive part (accounts, servers,
	// references) and the commands that back it up and restore it.
	Private private.Settings
	// Files are the other files of the private archive.
	Files []string
}

const DefaultTTL = 30 * time.Minute

// StoresPath is ~/.config/ai-secrets/config.toml.
func StoresPath(home string) string {
	return filepath.Join(home, ".config", "ai-secrets", "config.toml")
}

// storesFile is config.toml and its private file.
type storesFile struct {
	TTL    string `toml:"ttl"`
	Stores struct {
		OnePassword struct {
			Read string `toml:"read"`
		} `toml:"1password"`
		Bitwarden map[string]struct {
			Server        string `toml:"server"`
			Unlock        string `toml:"unlock"`
			Password      string `toml:"password"`
			LockAfterRead *bool  `toml:"lock_after_read"`
			DataDir       string `toml:"data_dir"`
		} `toml:"bitwarden"`
	} `toml:"stores"`
	Private *private.Settings `toml:"private"`
	Files   []string          `toml:"files"`
}

func LoadStores(home string) (*Stores, error) {
	path := StoresPath(home)
	var raw storesFile
	if _, err := toml.DecodeFile(path, &raw); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	if len(raw.Files) > 0 {
		return nil, fmt.Errorf("%s: files belongs in the private file ([private] file)", path)
	}
	var settings private.Settings
	if raw.Private != nil && raw.Private.File != "" {
		settings = *raw.Private
		settings.File = expandHome(settings.File, home)
		if err := mergePrivate(&raw, path, settings.File); err != nil {
			return nil, err
		}
	}
	s := &Stores{Path: path, TTL: DefaultTTL, OpIsolated: true, Bitwarden: map[string]*Bitwarden{}, Private: settings}
	for _, f := range raw.Files {
		s.Files = append(s.Files, expandHome(f, home))
	}
	if raw.TTL != "" {
		d, err := time.ParseDuration(raw.TTL)
		if err != nil || d <= 0 {
			return nil, fmt.Errorf("%s: ttl = %q is not a duration (e.g. \"30m\", \"2h\")", path, raw.TTL)
		}
		s.TTL = d
	}
	switch raw.Stores.OnePassword.Read {
	case "", "isolated":
	case "shared":
		s.OpIsolated = false
	default:
		return nil, fmt.Errorf("%s: stores.1password.read = %q: use \"isolated\" or \"shared\"", path, raw.Stores.OnePassword.Read)
	}
	for name, b := range raw.Stores.Bitwarden {
		acc := &Bitwarden{
			Name:          name,
			Server:        b.Server,
			Unlock:        b.Unlock,
			Password:      b.Password,
			LockAfterRead: b.LockAfterRead == nil || *b.LockAfterRead,
			DataDir:       b.DataDir,
		}
		if acc.Unlock == "" {
			acc.Unlock = "password"
		}
		if acc.DataDir == "" {
			acc.DataDir = filepath.Join(home, ".config", "ai-secrets", "bitwarden", name)
		} else {
			acc.DataDir = expandHome(acc.DataDir, home)
		}
		switch acc.Unlock {
		case "password":
			if !IsOp(acc.Password) {
				return nil, fmt.Errorf("%s: stores.bitwarden.%s: password must be an op:// reference to the master password in 1Password", path, name)
			}
		case "biometric":
		default:
			return nil, fmt.Errorf("%s: stores.bitwarden.%s: unlock = %q: use \"password\" or \"biometric\"", path, name, acc.Unlock)
		}
		s.Bitwarden[name] = acc
	}
	return s, nil
}

// mergePrivate adds the private file to raw; each setting and account is
// defined in one file only.
func mergePrivate(raw *storesFile, path, file string) error {
	if _, err := os.Stat(file); errors.Is(err, os.ErrNotExist) {
		return &private.MissingError{File: file}
	}
	var priv storesFile
	if _, err := toml.DecodeFile(file, &priv); err != nil {
		return fmt.Errorf("reading %s: %w", file, err)
	}
	both := func(what string) error {
		return fmt.Errorf("%s is set in both %s and %s: keep one", what, path, file)
	}
	if priv.Private != nil {
		return fmt.Errorf("%s: [private] belongs in %s", file, path)
	}
	if priv.TTL != "" {
		if raw.TTL != "" {
			return both("ttl")
		}
		raw.TTL = priv.TTL
	}
	if r := priv.Stores.OnePassword.Read; r != "" {
		if raw.Stores.OnePassword.Read != "" {
			return both("stores.1password.read")
		}
		raw.Stores.OnePassword.Read = r
	}
	if raw.Stores.Bitwarden == nil {
		raw.Stores.Bitwarden = priv.Stores.Bitwarden
	} else {
		for name, b := range priv.Stores.Bitwarden {
			if _, dup := raw.Stores.Bitwarden[name]; dup {
				return both("stores.bitwarden." + name)
			}
			raw.Stores.Bitwarden[name] = b
		}
	}
	raw.Files = priv.Files
	return nil
}

func expandHome(p, home string) string {
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(home, p[2:])
	}
	return p
}

// SplitBitwardenRef reads bw://<account>/<item>: the first segment is an
// account when one with that name is configured, otherwise the whole path is
// the item.
func SplitBitwardenRef(ref string, accounts map[string]*Bitwarden) (account, item string) {
	rest := strings.TrimPrefix(ref, "bw://")
	if a, i, ok := strings.Cut(rest, "/"); ok && accounts[a] != nil {
		return a, i
	}
	return "", rest
}
