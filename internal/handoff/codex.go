package handoff

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// FindCodexSession finds the rollout file of session id in a CODEX_HOME:
// sessions/<year>/<month>/<day>/rollout-<time>-<id>.jsonl. Inside Codex the id
// is in CODEX_SESSION_ID.
func FindCodexSession(codexHome, id string) (string, error) {
	if id == "" {
		return "", errors.New("no Codex session id (CODEX_SESSION_ID is not set)")
	}
	var found string
	filepath.WalkDir(filepath.Join(codexHome, "sessions"), func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(d.Name(), "-"+id+".jsonl") && strings.HasPrefix(d.Name(), "rollout-") {
			found = p
			return filepath.SkipAll
		}
		return nil
	})
	if found == "" {
		return "", fmt.Errorf("Codex session %s not found in %s", id, codexHome)
	}
	return found, nil
}

type codexItem struct {
	Type    string `json:"type"`
	Payload struct {
		Type    string `json:"type"`
		Role    string `json:"role"`
		Name    string `json:"name"`
		Input   string `json:"input"`
		Args    string `json:"arguments"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"payload"`
}

var patchFile = regexp.MustCompile(`(?m)^\*\*\* (?:Update|Add|Delete) File: (.+)$`)

// injected are the user messages Codex adds itself: instructions, context and
// the text of the skills a request invokes.
func injected(msg string) bool {
	return strings.HasPrefix(msg, "# AGENTS.md instructions") || strings.HasPrefix(msg, "<environment_context>") ||
		strings.HasPrefix(msg, "<user_instructions>") || strings.HasPrefix(msg, "<skill>")
}

// SummaryCodex describes a Codex session for another agent, like Summary
// does for Claude Code.
func SummaryCodex(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	var goal string
	var turns, files []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 64*1024*1024)
	for sc.Scan() {
		var it codexItem
		if json.Unmarshal(sc.Bytes(), &it) != nil || it.Type != "response_item" {
			continue
		}
		p := it.Payload
		switch {
		case p.Type == "message" && (p.Role == "user" || p.Role == "assistant"):
			var parts []string
			for _, c := range p.Content {
				if (c.Type == "input_text" || c.Type == "output_text") && strings.TrimSpace(c.Text) != "" {
					parts = append(parts, c.Text)
				}
			}
			msg := strings.TrimSpace(strings.Join(parts, "\n"))
			if msg == "" || (p.Role == "user" && injected(msg)) {
				continue
			}
			if p.Role == "user" {
				if goal == "" {
					goal = msg
					continue
				}
				turns = append(turns, "**Request:** "+msg)
			} else {
				turns = append(turns, "**Answer:** "+msg)
			}
		case p.Name == "apply_patch":
			patch := p.Input
			if patch == "" {
				var a struct {
					Input string `json:"input"`
				}
				json.Unmarshal([]byte(p.Args), &a)
				patch = a.Input
			}
			for _, m := range patchFile.FindAllStringSubmatch(patch, -1) {
				if f := strings.TrimSpace(m[1]); !slices.Contains(files, f) {
					files = append(files, f)
				}
			}
		}
	}
	if err := sc.Err(); err != nil {
		return "", err
	}
	if goal == "" {
		return "", errors.New("the Codex session contains no requests")
	}
	return render(goal, turns, files, path), nil
}
