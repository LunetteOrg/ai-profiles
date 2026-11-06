package handoff

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const codexRollout = `{"timestamp":"2026-10-22T13:00:00Z","type":"session_meta","payload":{"id":"01a1-abc"}}
{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"# AGENTS.md instructions for /r\n..."}]}}
{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"<environment_context>\n  <cwd>/r</cwd>\n</environment_context>"}]}}
{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"Aggiungi il login con Google"}]}}
{"type":"response_item","payload":{"type":"reasoning","summary":[]}}
{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Parto dal controller."}]}}
{"type":"response_item","payload":{"type":"custom_tool_call","name":"apply_patch","input":"*** Begin Patch\n*** Update File: app/auth.go\n@@\n-x\n+y\n*** Add File: app/google.go\n+package app\n*** End Patch"}}
{"type":"response_item","payload":{"type":"function_call","name":"apply_patch","arguments":"{\"input\":\"*** Begin Patch\\n*** Update File: app/auth.go\\n*** End Patch\"}"}}
{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"ora i test"}]}}
{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"<skill>\n<name>handoff</name>\n</skill>"}]}}
{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Test scritti, due falliscono."}]}}
`

func TestFindCodexSession(t *testing.T) {
	home := t.TempDir()
	p := filepath.Join(home, "sessions", "2026", "10", "22", "rollout-2026-10-22T13-00-00-01a1-abc.jsonl")
	write(t, p, codexRollout)
	write(t, filepath.Join(home, "sessions", "2026", "10", "22", "rollout-2026-10-22T12-00-00-altro.jsonl"), "{}\n")
	if got, err := FindCodexSession(home, "01a1-abc"); err != nil || got != p {
		t.Errorf("FindCodexSession = %q, %v", got, err)
	}
	if _, err := FindCodexSession(home, "manca"); err == nil {
		t.Errorf("an unknown id is an error")
	}
	if _, err := FindCodexSession(home, ""); err == nil {
		t.Errorf("an empty id is an error")
	}
}

func TestSummaryCodex(t *testing.T) {
	p := filepath.Join(t.TempDir(), "rollout.jsonl")
	os.WriteFile(p, []byte(codexRollout), 0o600)
	s, err := SummaryCodex(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Aggiungi il login con Google", "ora i test", "Test scritti, due falliscono.", "app/auth.go", "app/google.go"} {
		if !strings.Contains(s, want) {
			t.Errorf("summary misses %q:\n%s", want, s)
		}
	}
	for _, unwanted := range []string{"AGENTS.md instructions", "environment_context", "<skill>"} {
		if strings.Contains(s, unwanted) {
			t.Errorf("injected context %q must stay out:\n%s", unwanted, s)
		}
	}
	if strings.Count(s, "app/auth.go") != 1 {
		t.Errorf("each file once:\n%s", s)
	}
}
