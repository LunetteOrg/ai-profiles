// Package secretagent keeps secrets read from 1Password or Bitwarden in
// memory for a while, like ssh-agent: one approval covers every program that
// needs them until they expire. Values never touch the disk.
package secretagent

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/LunetteOrg/ai-profiles/internal/secrets"
)

// Request is one line of the protocol. Refs maps variable names to
// references; Bitwarden is the account for bw:// references.
type Request struct {
	Op     string            `json:"op"` // get, status, lock, stop
	Reason string            `json:"reason,omitempty"`
	Refs   map[string]string `json:"refs,omitempty"`
	// Account is the Bitwarden account of bw:// references that name none.
	Account string `json:"account,omitempty"`
}

type Entry struct {
	Name    string    `json:"name"`
	Ref     string    `json:"ref"`
	Expires time.Time `json:"expires"`
}

type Response struct {
	Values  map[string]string `json:"values,omitempty"`
	Entries []Entry           `json:"entries,omitempty"`
	Error   string            `json:"error,omitempty"`
}

type cached struct {
	names   []string // every variable name the reference was asked as
	ref     string
	value   string
	expires time.Time
}

// Agent holds the values. Resolve reads one reference (1Password, Bitwarden);
// Notify tells the user why a password manager is about to ask.
type Agent struct {
	TTL     time.Duration
	Resolve func(ref, account string) (string, error)
	Notify  func(msg string)
	Now     func() time.Time
	Stop    func()
	// Clear, when set, runs on lock and stop: the Bitwarden sessions kept by
	// Resolve go with the secrets.
	Clear func()
	// Hold and Release, when set, wrap the reads of one request, so values
	// asked together share one password-manager login.
	Hold, Release func()

	mu    sync.Mutex
	cache map[string]cached
}

func key(ref, account string) string {
	if strings.HasPrefix(ref, "bw://") && account != "" {
		return account + "|" + ref
	}
	return ref
}

// Handle answers one request. Requests are served one at a time, so two
// programs starting together trigger a single approval.
func (a *Agent) Handle(req Request) Response {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cache == nil {
		a.cache = map[string]cached{}
	}
	now := a.Now()
	for k, c := range a.cache {
		if !now.Before(c.expires) {
			delete(a.cache, k)
		}
	}
	switch req.Op {
	case "status":
		var out []Entry
		for _, c := range a.cache {
			out = append(out, Entry{Name: strings.Join(c.names, ", "), Ref: c.ref, Expires: c.expires})
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
		return Response{Entries: out}
	case "lock":
		a.cache = map[string]cached{}
		if a.Clear != nil {
			a.Clear()
		}
		return Response{}
	case "stop":
		a.cache = map[string]cached{}
		if a.Clear != nil {
			a.Clear()
		}
		if a.Stop != nil {
			go a.Stop()
		}
		return Response{}
	case "get":
		return a.get(req, now)
	}
	return Response{Error: fmt.Sprintf("unknown request %q", req.Op)}
}

func (a *Agent) get(req Request, now time.Time) Response {
	values := map[string]string{}
	var missing []string
	for _, name := range slices.Sorted(maps.Keys(req.Refs)) {
		ref := req.Refs[name]
		if !secrets.IsOp(ref) && !strings.HasPrefix(ref, "bw://") {
			return Response{Error: fmt.Sprintf("%s: %q is not an op:// or bw:// reference", name, ref)}
		}
		if c, ok := a.cache[key(ref, req.Account)]; ok {
			values[name] = c.value
			if !slices.Contains(c.names, name) {
				c.names = append(c.names, name)
				a.cache[key(ref, req.Account)] = c
			}
		} else {
			missing = append(missing, name)
		}
	}
	if len(missing) == 0 {
		return Response{Values: values}
	}
	if a.Notify != nil {
		reason := req.Reason
		if reason == "" {
			reason = "a program"
		}
		a.Notify(fmt.Sprintf("%s asks for %s (valid for %s)", reason, strings.Join(missing, ", "), human(a.TTL)))
	}
	expires := now.Add(a.TTL)
	if a.Hold != nil {
		a.Hold()
	}
	if a.Release != nil {
		defer a.Release()
	}
	for _, name := range missing {
		ref := req.Refs[name]
		v, err := a.Resolve(ref, req.Account)
		if err != nil {
			return Response{Error: fmt.Sprintf("%s: %v", name, err)}
		}
		a.cache[key(ref, req.Account)] = cached{names: []string{name}, ref: ref, value: v, expires: expires}
		values[name] = v
	}
	return Response{Values: values}
}

func human(d time.Duration) string {
	switch {
	case d%time.Hour == 0:
		return fmt.Sprintf("%d h", int(d.Hours()))
	case d%time.Minute == 0:
		return fmt.Sprintf("%d min", int(d.Minutes()))
	}
	return d.String()
}

// Listen opens the agent's socket in a folder only the user can enter.
func Listen(socket string) (net.Listener, error) {
	dir := filepath.Dir(socket)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, err
	}
	os.Remove(socket) // a stale socket of an agent that is gone
	ln, err := net.Listen("unix", socket)
	if err != nil {
		return nil, err
	}
	os.Chmod(socket, 0o600)
	return ln, nil
}

// Serve answers connections until the listener closes: one JSON request and
// one JSON response per line.
func (a *Agent) Serve(ln net.Listener) error {
	for {
		conn, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		go func() {
			defer conn.Close()
			sc := bufio.NewScanner(conn)
			sc.Buffer(make([]byte, 64*1024), 1024*1024)
			enc := json.NewEncoder(conn)
			for sc.Scan() {
				var req Request
				if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
					enc.Encode(Response{Error: "unreadable request"})
					continue
				}
				enc.Encode(a.Handle(req))
			}
		}()
	}
}
