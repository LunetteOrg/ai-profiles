package apply

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStableToolPath(t *testing.T) {
	data := t.TempDir()
	installed := filepath.Join(data, "installs", "github-lunetteorg-ai-profiles", "0.1.0", "ai-secrets")
	shim := filepath.Join(data, "shims", "ai-secrets")
	for _, p := range []string{installed, shim} {
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755)
	}
	getenv := func(k string) string { return map[string]string{"MISE_DATA_DIR": data}[k] }

	if got := StableToolPath(installed, getenv); got != shim {
		t.Errorf("a tool installed by mise should be written as its shim, got %s", got)
	}
	other := "/opt/homebrew/bin/ai-secrets"
	if got := StableToolPath(other, getenv); got != other {
		t.Errorf("a path outside mise should stay, got %s", got)
	}
	os.Remove(shim)
	if got := StableToolPath(installed, getenv); got != installed {
		t.Errorf("without a shim the installed path should stay, got %s", got)
	}
}
