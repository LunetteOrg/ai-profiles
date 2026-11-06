package secretagent

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeBackend struct {
	mu    sync.Mutex
	calls []string
	fail  map[string]bool
}

func (f *fakeBackend) resolve(ref, account string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, ref)
	if f.fail[ref] {
		return "", errors.New("negato")
	}
	return "valore:" + ref, nil
}

func newAgent(t *testing.T) (*Agent, *fakeBackend, *[]string, *time.Time) {
	t.Helper()
	b := &fakeBackend{fail: map[string]bool{}}
	var notes []string
	now := time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)
	a := &Agent{
		TTL:     30 * time.Minute,
		Resolve: b.resolve,
		Notify:  func(msg string) { notes = append(notes, msg) },
		Now:     func() time.Time { return now },
	}
	return a, b, &notes, &now
}

func TestAgentCachesUntilExpiry(t *testing.T) {
	a, b, notes, now := newAgent(t)
	req := Request{Op: "get", Reason: "server budget, profile personal",
		Refs: map[string]string{"BUDGET_API_KEY": "op://Private/x/credential", "ALTRA": "op://Private/y/credential"}}

	resp := a.Handle(req)
	if resp.Error != "" || resp.Values["BUDGET_API_KEY"] != "valore:op://Private/x/credential" {
		t.Fatalf("resp = %+v", resp)
	}
	if len(*notes) != 1 || !strings.Contains((*notes)[0], "server budget, profile personal") ||
		!strings.Contains((*notes)[0], "BUDGET_API_KEY") || !strings.Contains((*notes)[0], "30 min") {
		t.Errorf("one notification with reason, keys and validity: %v", *notes)
	}

	*now = now.Add(29 * time.Minute)
	a.Handle(req)
	if len(b.calls) != 2 || len(*notes) != 1 {
		t.Errorf("within the validity nothing is asked again: calls %v, notes %v", b.calls, *notes)
	}

	*now = now.Add(2 * time.Minute)
	a.Handle(req)
	if len(b.calls) != 4 || len(*notes) != 2 {
		t.Errorf("after expiry both are asked again: calls %v, notes %v", b.calls, *notes)
	}
}

func TestAgentStatusLockAndErrors(t *testing.T) {
	a, b, _, _ := newAgent(t)
	a.Handle(Request{Op: "get", Reason: "r", Refs: map[string]string{"K": "op://a/b/c"}})
	st := a.Handle(Request{Op: "status"})
	if len(st.Entries) != 1 || st.Entries[0].Name != "K" || st.Entries[0].Ref != "op://a/b/c" {
		t.Errorf("status = %+v", st.Entries)
	}
	for _, e := range st.Entries {
		if strings.Contains(e.Ref+e.Name, "valore") {
			t.Errorf("status must never contain values")
		}
	}
	a.Handle(Request{Op: "get", Reason: "r", Refs: map[string]string{"ALTRO_NOME": "op://a/b/c"}})
	if st := a.Handle(Request{Op: "status"}); st.Entries[0].Name != "K, ALTRO_NOME" {
		t.Errorf("status lists every name of a reference: %q", st.Entries[0].Name)
	}
	a.Handle(Request{Op: "lock"})
	if st := a.Handle(Request{Op: "status"}); len(st.Entries) != 0 {
		t.Errorf("lock clears everything: %+v", st.Entries)
	}

	b.fail["op://a/b/negato"] = true
	resp := a.Handle(Request{Op: "get", Reason: "r", Refs: map[string]string{"N": "op://a/b/negato"}})
	if resp.Error == "" || len(resp.Values) != 0 {
		t.Errorf("a denied approval is an error and gives no values: %+v", resp)
	}
	if resp := a.Handle(Request{Op: "get", Refs: map[string]string{"X": "uvx"}}); resp.Error == "" {
		t.Errorf("a value that is not a reference is refused")
	}
}

func TestClientAgentOverSocket(t *testing.T) {
	dir, err := os.MkdirTemp("", "ais")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	sock := filepath.Join(dir, "a.sock")
	a, _, _, _ := newAgent(t)
	ln, err := Listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	go a.Serve(ln)
	defer ln.Close()

	if info, err := os.Stat(dir); err != nil || info.Mode().Perm() != 0o700 {
		t.Errorf("the socket folder must be private: %v %v", info.Mode(), err)
	}
	c := &Client{Socket: sock}
	vals, err := c.Get("motivo", map[string]string{"K": "op://a/b/c"}, "")
	if err != nil || vals["K"] != "valore:op://a/b/c" {
		t.Errorf("Get = %v, %v", vals, err)
	}
	if _, err := (&Client{Socket: filepath.Join(dir, "manca.sock")}).Get("m", map[string]string{"K": "op://a/b/c"}, ""); err == nil {
		t.Errorf("no agent and no way to start it is an error")
	}
	var _ net.Listener = ln
}
