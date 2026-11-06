// Package private keeps the files with sensitive data out of a configuration
// that can be published (in a dotfiles repo, for example): it packs them into
// one archive that the user's own commands store and fetch, such as a
// password manager document.
//
//	[private]
//	file    = "~/.config/<tool>/private.toml"
//	restore = "op document get <tool> --vault Work"    # archive on stdout
//	backup  = "op document edit <tool> --vault Work -" # archive on stdin
package private

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
)

// Settings is the [private] section of a configuration.
type Settings struct {
	File    string `toml:"file"`
	Restore string `toml:"restore"`
	Backup  string `toml:"backup"`
}

// MissingError reports a configured private file that is not on disk.
type MissingError struct{ File string }

func (e *MissingError) Error() string { return e.File + " is missing" }

// Section reads the [private] section of the configuration at path.
func Section(path, home string) (Settings, error) {
	var raw struct {
		Private Settings `toml:"private"`
	}
	if _, err := toml.DecodeFile(path, &raw); err != nil {
		return Settings{}, fmt.Errorf("reading %s: %w", path, err)
	}
	s := raw.Private
	if s.File != "" {
		s.File = expand(s.File, home)
	}
	return s, nil
}

// Result tells what Unpack did with each file of the archive.
type Result struct {
	Written []string // created or overwritten
	Same    []string // already there with the same content
	Kept    []string // already there with other content, left as they are
}

// Pack builds a gzipped tar of files, named by their path under home.
func Pack(home string, files []string) ([]byte, error) {
	entries := map[string][]byte{}
	for _, f := range files {
		p := expand(f, home)
		rel, err := filepath.Rel(home, p)
		if err != nil || !filepath.IsLocal(rel) {
			return nil, fmt.Errorf("%s is outside the home folder", f)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		entries[filepath.ToSlash(rel)] = b
	}
	return packEntries(entries)
}

func packEntries(entries map[string][]byte) ([]byte, error) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, name := range slices.Sorted(maps.Keys(entries)) {
		b := entries[name]
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(b)), Typeflag: tar.TypeReg}); err != nil {
			return nil, err
		}
		if _, err := tw.Write(b); err != nil {
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Unpack writes the files of the archive under home. A file already there
// with other content is kept unless force is set.
func Unpack(home string, data []byte, force bool) (Result, error) {
	entries, err := unpackEntries(data)
	if err != nil {
		return Result{}, err
	}
	var res Result
	for _, name := range slices.Sorted(maps.Keys(entries)) {
		p := filepath.Join(home, filepath.FromSlash(name))
		b := entries[name]
		if cur, err := os.ReadFile(p); err == nil {
			if bytes.Equal(cur, b) {
				res.Same = append(res.Same, p)
				continue
			}
			if !force {
				res.Kept = append(res.Kept, p)
				continue
			}
		}
		if err := writeFile(p, b); err != nil {
			return res, err
		}
		res.Written = append(res.Written, p)
	}
	return res, nil
}

func unpackEntries(data []byte) (map[string][]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("the archive is not a gzipped tar: %w", err)
	}
	tr := tar.NewReader(gz)
	entries := map[string][]byte{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading the archive: %w", err)
		}
		if h.Typeflag != tar.TypeReg {
			continue
		}
		if !filepath.IsLocal(filepath.FromSlash(h.Name)) {
			return nil, fmt.Errorf("the archive holds %q, outside the home folder", h.Name)
		}
		b, err := io.ReadAll(tr)
		if err != nil {
			return nil, err
		}
		entries[h.Name] = b
	}
	return entries, nil
}

// Save packs the private file and files, hands the archive to the backup
// command and records what it saved, for Changed.
func (s Settings) Save(home string, files []string) error {
	if s.Backup == "" {
		return errors.New("no backup command in [private]")
	}
	all := append([]string{s.File}, files...)
	data, err := Pack(home, all)
	if err != nil {
		return err
	}
	cmd := exec.Command("sh", "-c", s.Backup)
	cmd.Stdin = bytes.NewReader(data)
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("backup command (%s): %w", s.Backup, err)
	}
	return s.record(home, all)
}

// Fetch runs the restore command and unpacks the archive it prints.
func (s Settings) Fetch(home string, force bool) (Result, error) {
	if s.Restore == "" {
		return Result{}, errors.New("no restore command in [private]")
	}
	cmd := exec.Command("sh", "-c", s.Restore)
	cmd.Stderr = os.Stderr
	data, err := cmd.Output()
	if err != nil {
		return Result{}, fmt.Errorf("restore command (%s): %w", s.Restore, err)
	}
	res, err := Unpack(home, data, force)
	if err != nil {
		return res, err
	}
	if len(res.Kept) == 0 {
		entries, _ := unpackEntries(data)
		var all []string
		for name := range entries {
			all = append(all, filepath.Join(home, filepath.FromSlash(name)))
		}
		err = s.record(home, all)
	}
	return res, err
}

// Changed lists the private file and files whose content differs from the
// last backup or restore; without one, all of them.
func (s Settings) Changed(home string, files []string) ([]string, error) {
	saved := map[string]string{}
	if b, err := os.ReadFile(s.stamp()); err == nil {
		if err := json.Unmarshal(b, &saved); err != nil {
			return nil, fmt.Errorf("%s: %w", s.stamp(), err)
		}
	}
	var changed []string
	for _, f := range append([]string{s.File}, files...) {
		p := expand(f, home)
		if h, ok := saved[p]; !ok || h != hash(p) {
			changed = append(changed, p)
		}
	}
	return changed, nil
}

// record writes the hash of each file, next to the private file.
func (s Settings) record(home string, files []string) error {
	saved := map[string]string{}
	for _, f := range files {
		p := expand(f, home)
		saved[p] = hash(p)
	}
	b, err := json.MarshalIndent(saved, "", "  ")
	if err != nil {
		return err
	}
	return writeFile(s.stamp(), append(b, '\n'))
}

func (s Settings) stamp() string {
	return filepath.Join(filepath.Dir(s.File), "."+filepath.Base(s.File)+".backup")
}

func hash(p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func writeFile(p string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), "."+filepath.Base(p)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), p)
}

func expand(p, home string) string {
	if p == "~" {
		return home
	}
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(home, p[2:])
	}
	return filepath.Clean(p)
}
