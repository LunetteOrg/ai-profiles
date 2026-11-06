// Package handoff continues work elsewhere: a Claude Code session in another
// profile, or the same work with another agent, from a summary built locally
// from the session file (no tokens needed, so it works when they ran out).
package handoff

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

var nonAlnum = regexp.MustCompile(`[^a-zA-Z0-9]`)

// ProjectDir is the folder name Claude Code gives a working directory under
// projects/.
func ProjectDir(cwd string) string {
	return nonAlnum.ReplaceAllString(cwd, "-")
}

// FindSession returns the session file of id, or the newest one, for cwd in
// the Claude Code profile folder.
func FindSession(profileDir, cwd, id string) (string, error) {
	var dirs []string
	for _, c := range candidates(cwd) {
		dirs = append(dirs, filepath.Join(profileDir, "projects", ProjectDir(c)))
	}
	if id != "" {
		for _, d := range dirs {
			p := filepath.Join(d, id+".jsonl")
			if _, err := os.Stat(p); err == nil {
				return p, nil
			}
		}
		return "", fmt.Errorf("session %s not found for %s in %s", id, cwd, profileDir)
	}
	var best string
	var bestInfo fs.FileInfo
	for _, d := range dirs {
		files, _ := filepath.Glob(filepath.Join(d, "*.jsonl"))
		for _, f := range files {
			info, err := os.Stat(f)
			if err == nil && (bestInfo == nil || info.ModTime().After(bestInfo.ModTime())) {
				best, bestInfo = f, info
			}
		}
	}
	if best == "" {
		return "", fmt.Errorf("no Claude session for %s in %s", cwd, profileDir)
	}
	return best, nil
}

// candidates are cwd as given and with symlinks resolved: Claude Code may
// have recorded either.
func candidates(cwd string) []string {
	out := []string{cwd}
	if real, err := filepath.EvalSymlinks(cwd); err == nil && real != cwd {
		out = append(out, real)
	}
	return out
}

// CopySession copies a session file, and its folder of the same name, into
// another profile for cwd, and returns the session id. Files already there are
// left as they are.
func CopySession(path, dstProfileDir, cwd string) (string, error) {
	id := strings.TrimSuffix(filepath.Base(path), ".jsonl")
	dst := filepath.Join(dstProfileDir, "projects", ProjectDir(cwd))
	if err := copyFile(path, filepath.Join(dst, id+".jsonl")); err != nil {
		return "", err
	}
	side := strings.TrimSuffix(path, ".jsonl")
	if info, err := os.Stat(side); err == nil && info.IsDir() {
		err := filepath.WalkDir(side, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			rel, _ := filepath.Rel(side, p)
			return copyFile(p, filepath.Join(dst, id, rel))
		})
		if err != nil {
			return "", err
		}
	}
	return id, nil
}

func copyFile(src, dst string) error {
	if _, err := os.Stat(dst); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

type entry struct {
	Type    string `json:"type"`
	Message struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

type block struct {
	Type  string `json:"type"`
	Text  string `json:"text"`
	Name  string `json:"name"`
	Input struct {
		FilePath string `json:"file_path"`
	} `json:"input"`
}

// recent is how many of the last requests and answers the summary keeps.
const recent = 4

// Summary describes the work of a Claude Code session for another agent: the
// first request, the last requests and answers, the files written.
func Summary(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	var goal string
	var turns []string
	var files []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 64*1024*1024)
	for sc.Scan() {
		var e entry
		if json.Unmarshal(sc.Bytes(), &e) != nil || (e.Type != "user" && e.Type != "assistant") {
			continue
		}
		var text string
		var blocks []block
		if json.Unmarshal(e.Message.Content, &text) != nil {
			json.Unmarshal(e.Message.Content, &blocks)
		}
		var parts []string
		if text != "" {
			parts = append(parts, text)
		}
		for _, b := range blocks {
			switch {
			case b.Type == "text" && strings.TrimSpace(b.Text) != "":
				parts = append(parts, b.Text)
			case b.Type == "tool_use" && b.Input.FilePath != "" && (b.Name == "Edit" || b.Name == "Write" || b.Name == "MultiEdit" || b.Name == "NotebookEdit"):
				if !slices.Contains(files, b.Input.FilePath) {
					files = append(files, b.Input.FilePath)
				}
			}
		}
		if len(parts) == 0 {
			continue
		}
		msg := strings.TrimSpace(strings.Join(parts, "\n"))
		if e.Type == "user" {
			if goal == "" {
				goal = msg
				continue
			}
			turns = append(turns, "**Request:** "+msg)
		} else {
			turns = append(turns, "**Answer:** "+msg)
		}
	}
	if err := sc.Err(); err != nil {
		return "", err
	}
	if goal == "" {
		return "", errors.New("the session contains no requests")
	}
	return render(goal, turns, files, path), nil
}

// render writes the summary: goal, the last exchanges, the files changed.
func render(goal string, turns, files []string, path string) string {
	if len(turns) > 2*recent {
		turns = turns[len(turns)-2*recent:]
	}
	var b strings.Builder
	b.WriteString("I am continuing work started in another session. Summary:\n\n")
	fmt.Fprintf(&b, "## Goal\n\n%s\n\n", goal)
	if len(turns) > 0 {
		fmt.Fprintf(&b, "## Latest exchanges\n\n%s\n\n", strings.Join(turns, "\n\n"))
	}
	if len(files) > 0 {
		b.WriteString("## Files changed\n\n")
		for _, f := range files {
			fmt.Fprintf(&b, "- %s\n", f)
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "The full session is in %s. Check the state of the files and carry on.\n", path)
	return b.String()
}
