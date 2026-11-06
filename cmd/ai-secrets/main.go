// ai-secrets keeps secrets read from 1Password or Bitwarden in memory for a
// while, so one approval covers every program that needs them: MCP servers
// of every agent session, agents launched by ai-profile.
//
//	ai-secrets run [--reason <reason>] -- <command> [<arguments>]
//
// replaces the op:// references in the environment with their values, asking
// the agent (started on demand), then runs the command.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/LunetteOrg/ai-profiles/internal/private"
	"github.com/LunetteOrg/ai-profiles/internal/secretagent"
	"github.com/LunetteOrg/ai-profiles/internal/secrets"
)

const helpText = `ai-secrets: 1Password and Bitwarden secrets kept in memory for a while.

  ai-secrets run [--reason <reason>] -- <command> [<arguments>]
                       replaces the op:// and bw://<account>/<item> references
                       in the environment with their values and runs the command
  ai-secrets status    secrets in memory and their expiry, without values
  ai-secrets lock      forgets every secret
  ai-secrets stop      stops the agent
  ai-secrets agent     the agent (starts by itself on the first request)
  ai-secrets backup    saves the private file and the files it lists with the
                       [private] backup command
  ai-secrets restore [--force]
                       brings them back with the [private] restore command;
                       files that differ stay as they are without --force

Validity (ttl) and how to reach each store: %s (default ttl %s).
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "ai-secrets:", err)
		os.Exit(1)
	}
}

func client(p secretagent.Paths) (*secretagent.Client, error) {
	self, err := os.Executable()
	if err != nil {
		return nil, err
	}
	return &secretagent.Client{Socket: p.Socket, Start: func() error { return secretagent.Start(self, p) }}, nil
}

func run(args []string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	p := secretagent.PathsFor(home)
	if len(args) == 0 {
		return errors.New("usage: ai-secrets run|status|lock|stop (ai-secrets help)")
	}
	switch args[0] {
	case "help", "-h", "--help":
		fmt.Printf(helpText, p.Config, secrets.DefaultTTL)
		return nil
	case "agent":
		stores, err := loadStores(home)
		if err != nil {
			return err
		}
		return serve(p, home, stores)
	case "backup":
		if len(args) > 1 {
			return errors.New("usage: ai-secrets backup")
		}
		stores, err := secrets.LoadStores(home)
		if err != nil {
			return err
		}
		if stores.Private.File == "" {
			return fmt.Errorf("%s has no [private] section", stores.Path)
		}
		if err := stores.Private.Save(home, stores.Files); err != nil {
			return err
		}
		for _, f := range append([]string{stores.Private.File}, stores.Files...) {
			fmt.Println("saved", f)
		}
		return nil
	case "restore":
		force := false
		for _, a := range args[1:] {
			if a != "--force" {
				return errors.New("usage: ai-secrets restore [--force]")
			}
			force = true
		}
		s, err := private.Section(secrets.StoresPath(home), home)
		if err != nil {
			return err
		}
		if s.File == "" {
			return fmt.Errorf("%s has no [private] section", secrets.StoresPath(home))
		}
		res, err := s.Fetch(home, force)
		if err != nil {
			return err
		}
		printRestore(os.Stdout, res)
		return nil
	case "run":
		return runCommand(p, args[1:])
	case "status":
		resp, err := (&secretagent.Client{Socket: p.Socket}).Do(secretagent.Request{Op: "status"})
		if err != nil {
			fmt.Println("agent not running: no secrets in memory")
			return nil
		}
		if len(resp.Entries) == 0 {
			fmt.Println("no secrets in memory")
			return nil
		}
		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "variable\treference\texpires")
		for _, e := range resp.Entries {
			fmt.Fprintf(tw, "%s\t%s\t%s\n", e.Name, e.Ref, e.Expires.Local().Format("15:04"))
		}
		return tw.Flush()
	case "lock", "stop":
		if _, err := (&secretagent.Client{Socket: p.Socket}).Do(secretagent.Request{Op: args[0]}); err != nil {
			fmt.Println("agent not running")
		}
		return nil
	}
	return fmt.Errorf("unknown command %q (ai-secrets help)", args[0])
}

// loadStores reads the configuration; when its private file is missing (on a
// new Mac, for example) it restores it first with the [private] restore
// command.
func loadStores(home string) (*secrets.Stores, error) {
	stores, err := secrets.LoadStores(home)
	var missing *private.MissingError
	if !errors.As(err, &missing) {
		return stores, err
	}
	s, err := private.Section(secrets.StoresPath(home), home)
	if err != nil {
		return nil, err
	}
	if s.Restore == "" {
		return nil, fmt.Errorf("%s is missing and [private] has no restore command", missing.File)
	}
	fmt.Fprintf(os.Stderr, "ai-secrets: %s is missing, restoring it with: %s\n", missing.File, s.Restore)
	res, err := s.Fetch(home, false)
	if err != nil {
		return nil, err
	}
	printRestore(os.Stderr, res)
	return secrets.LoadStores(home)
}

func printRestore(w io.Writer, res private.Result) {
	for _, p := range res.Written {
		fmt.Fprintf(w, "restored %s\n", p)
	}
	for _, p := range res.Same {
		fmt.Fprintf(w, "%s already matches the backup\n", p)
	}
	for _, p := range res.Kept {
		fmt.Fprintf(w, "%s differs from the backup and stays as it is (ai-secrets restore --force overwrites it)\n", p)
	}
}

func runCommand(p secretagent.Paths, args []string) error {
	const use = "usage: ai-secrets run [--reason <reason>] -- <command> [<arguments>]"
	reason := ""
	for len(args) > 0 && args[0] != "--" {
		if args[0] == "--reason" && len(args) > 1 {
			reason, args = args[1], args[2:]
			continue
		}
		return errors.New(use)
	}
	if len(args) < 2 {
		return errors.New(use)
	}
	cmd := args[1:]
	refs := map[string]string{}
	for _, kv := range os.Environ() {
		k, v, _ := strings.Cut(kv, "=")
		if secrets.IsOp(v) || strings.HasPrefix(v, "bw://") {
			refs[k] = v
		}
	}
	env := os.Environ()
	if len(refs) > 0 {
		c, err := client(p)
		if err != nil {
			return err
		}
		values, err := c.Get(reason, refs, "")
		if err != nil {
			return err
		}
		for i, kv := range env {
			k, _, _ := strings.Cut(kv, "=")
			if v, ok := values[k]; ok {
				env[i] = k + "=" + v
			}
		}
	}
	return execCommand(cmd, env)
}

func serve(p secretagent.Paths, home string, stores *secrets.Stores) error {
	release, ok, err := secretagent.Lock(p.Lock)
	if err != nil {
		return err
	}
	if !ok {
		return nil // another agent is already running
	}
	defer release()
	ln, err := secretagent.Listen(p.Socket)
	if err != nil {
		return err
	}
	// One resolver for the agent's life: it keeps the Bitwarden sessions of
	// accounts not locked after each read. The agent serves one request at
	// a time, so it needs no lock.
	r := &secrets.Resolver{Home: home, Getenv: os.Getenv, Run: secrets.Exec, RunIsolated: secrets.ExecIsolated, Stores: stores}
	a := &secretagent.Agent{
		TTL: stores.TTL,
		Resolve: func(ref, account string) (string, error) {
			r.Account = account
			return r.Resolve(ref)
		},
		Notify:  notify,
		Now:     time.Now,
		Stop:    func() { r.LockAll(); ln.Close() },
		Clear:   r.LockAll,
		Hold:    r.Hold,
		Release: r.Release,
	}
	return a.Serve(ln)
}
