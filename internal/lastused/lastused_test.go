package lastused

import (
	"os"
	"testing"
)

func TestSetGet(t *testing.T) {
	home := t.TempDir()
	if got := Get(home, "/x/playbook"); got != "" {
		t.Errorf("nothing recorded yet: %q", got)
	}
	if err := Set(home, "/x/playbook", "client-d"); err != nil {
		t.Fatal(err)
	}
	if err := Set(home, "/x/altro", "client-b"); err != nil {
		t.Fatal(err)
	}
	if got := Get(home, "/x/playbook"); got != "client-d" {
		t.Errorf("got %q", got)
	}
	if err := Set(home, "/x/playbook", "personal"); err != nil {
		t.Fatal(err)
	}
	if Get(home, "/x/playbook") != "personal" || Get(home, "/x/altro") != "client-b" {
		t.Errorf("a new record replaces only its folder's")
	}
	os.WriteFile(Path(home), []byte("non è json"), 0o600)
	if Get(home, "/x/playbook") != "" {
		t.Errorf("an unreadable file counts as no record")
	}
	if err := Set(home, "/x/playbook", "client-d"); err != nil || Get(home, "/x/playbook") != "client-d" {
		t.Errorf("an unreadable file is rewritten: %v", err)
	}
}
