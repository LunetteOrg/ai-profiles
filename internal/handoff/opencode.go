package handoff

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
)

// SummaryOpenCode describes an OpenCode session, as `opencode session export`
// prints it, for another agent; path is where that export was saved.
func SummaryOpenCode(export []byte, path string) (string, error) {
	var doc struct {
		Messages []struct {
			Type    string `json:"type"`
			Text    string `json:"text"`
			Content []struct {
				Type  string `json:"type"`
				Text  string `json:"text"`
				Name  string `json:"name"`
				State struct {
					Input struct {
						FilePath string `json:"filePath"`
						Path     string `json:"path"`
					} `json:"input"`
				} `json:"state"`
			} `json:"content"`
			Snapshot struct {
				Files []string `json:"files"`
			} `json:"snapshot"`
		} `json:"messages"`
	}
	// The CLI may print a line before the JSON.
	if i := strings.IndexByte(string(export), '{'); i > 0 {
		export = export[i:]
	}
	if err := json.Unmarshal(export, &doc); err != nil {
		return "", err
	}
	var goal string
	var turns, files []string
	addFile := func(f string) {
		if f != "" && !slices.Contains(files, f) {
			files = append(files, f)
		}
	}
	for _, m := range doc.Messages {
		switch m.Type {
		case "user":
			msg := strings.TrimSpace(m.Text)
			if msg == "" {
				continue
			}
			if goal == "" {
				goal = msg
				continue
			}
			turns = append(turns, "**Request:** "+msg)
		case "assistant":
			var parts []string
			for _, c := range m.Content {
				switch c.Type {
				case "text":
					if strings.TrimSpace(c.Text) != "" {
						parts = append(parts, c.Text)
					}
				case "tool":
					if c.Name == "edit" || c.Name == "write" || c.Name == "patch" || c.Name == "multiedit" {
						addFile(c.State.Input.FilePath)
						addFile(c.State.Input.Path)
					}
				}
			}
			for _, f := range m.Snapshot.Files {
				addFile(f)
			}
			if msg := strings.TrimSpace(strings.Join(parts, "\n")); msg != "" {
				turns = append(turns, "**Answer:** "+msg)
			}
		}
	}
	if goal == "" {
		return "", errors.New("the OpenCode session contains no requests")
	}
	return render(goal, turns, files, path), nil
}
