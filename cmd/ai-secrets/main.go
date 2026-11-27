// ai-secrets keeps secrets read from 1Password or Bitwarden in memory for a
// while, so one approval covers every program that needs them: MCP servers
// of every agent session, agents launched by ai-profile.
//
//	ai-secrets run [--reason <reason>] -- <command> [<arguments>]
//
// replaces the op:// references in the environment with their values, asking
// the agent (started on demand), then runs the command.
//
// A link named after a tool (scw → ai-secrets) is a tool shim: it runs the
// next tool of that name in PATH with the secrets of the folder's profile
// ([profiles.<p>.tools.<name>]) in that process's environment only.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/LunetteOrg/ai-profiles/internal/config"
	"github.com/LunetteOrg/ai-profiles/internal/lastused"
	"github.com/LunetteOrg/ai-profiles/internal/private"
	"github.com/LunetteOrg/ai-profiles/internal/secretagent"
	"github.com/LunetteOrg/ai-profiles/internal/secrets"
	"github.com/LunetteOrg/ai-profiles/internal/toolshim"
)

const helpText = `ai-secrets: 1Password and Bitwarden secrets kept in memory for a while.

  ai-secrets run [--reason <reason>] [--account <name>] -- <command> [<arguments>]
                       replaces the op:// and bw://<account>/<item> references
                       in the environment with their values and runs the command;
                       --account is the 1Password account ([stores.1password.<name>])
                       of op:// references without ?account=<name>; by default
                       the onepassword of the current folder's profile
  ai-secrets tool add <tool> --vault <vault> [--item <title>] [--profile <profile>]
                       [--account <1password account>] [--from <tool profile>]
                       moves the tool's key from its config file to a new
                       1Password item, writes the tool's section in the profile
                       and links its tool shim (tools: scw)
  ai-secrets bitwarden login <account>
                       logs the account's bw CLI in again ([stores.bitwarden.<account>]):
                       logout, server, login with the master password and, with
                       otp, the two-step code from 1Password; leaves it locked
  ai-secrets status    secrets in memory and their expiry, without values
  ai-secrets lock      forgets every secret
  ai-secrets stop      stops the agent
  ai-secrets agent     the agent (starts by itself on the first request)
  ai-secrets backup    saves the private file and the files it lists with the
                       [private] backup command
  ai-secrets restore [--force]
                       brings them back with the [private] restore command;
                       files that differ stay as they are without --force

A link named after a tool (scw → ai-secrets) is a tool shim: it runs the next
tool of that name in PATH with the env. references of [profiles.<p>.tools.<name>]
of the folder's profile resolved, or unchanged when the profile has none.

Validity (ttl) and how to reach each store: %s (default ttl %s).
`

func main() {
	run := run
	// Called by another name, ai-secrets is a tool shim: every argument
	// belongs to the tool.
	if name := strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe"); name != "ai-secrets" {
		run = func(args []string) error { return runShim(name, args) }
	}
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "ai-secrets:", err)
		os.Exit(1)
	}
}

// runShim runs the real tool name with the secrets of its section in the
// folder's profile, or unchanged when the profile does not declare it.
func runShim(name string, args []string) error {
	if !shimSupported {
		return fmt.Errorf("%s: tool shims are not available on Windows yet; remove the shim and use ai-secrets run", name)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	pathList := os.Getenv("PATH")
	real, err := toolshim.FindReal(name, pathList, toolshim.ShimDir(os.Args[0], pathList), self)
	if err != nil {
		return err
	}
	var cfg *config.Config
	if cfgPath := config.DefaultPath(home, os.Getenv); config.Exists(cfgPath) {
		if cfg, err = config.Load(cfgPath, home); err != nil {
			var missing *private.MissingError
			if errors.As(err, &missing) {
				return fmt.Errorf("%w: ai-profile restore brings it back", err)
			}
			return err
		}
		cfg.LastUsed = func(dir string) string { return lastused.Get(home, dir) }
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	s := &toolshim.Shim{
		Name: name, Real: real, Dir: cwd, Cfg: cfg, Environ: os.Environ(),
		Get: func(reason string, refs map[string]string, account string) (map[string]string, error) {
			c, err := client(secretagent.PathsFor(home))
			if err != nil {
				return nil, err
			}
			return c.Get(reason, refs, account)
		},
		Exec: func(bin string, args, env []string) error {
			return execPath(bin, append([]string{name}, args...), env)
		},
	}
	return s.Run(args)
}

func client(p secretagent.Paths) (*secretagent.Client, error) {
	self, err := os.Executable()
	if err != nil {
		return nil, err
	}
	// Called through a tool shim, the executable is the tool's link: the
	// agent starts from the file it points to, as ai-secrets.
	if real, err := filepath.EvalSymlinks(self); err == nil {
		self = real
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
		return runCommand(home, p, args[1:])
	case "tool":
		if len(args) < 2 || args[1] != "add" {
			return errors.New(toolAddUsage)
		}
		return runToolAdd(home, p, args[2:])
	case "bitwarden":
		if len(args) != 3 || args[1] != "login" {
			return errors.New(bitwardenLoginUsage)
		}
		stores, err := loadStores(home)
		if err != nil {
			return err
		}
		r := &secrets.Resolver{Home: home, Getenv: os.Getenv, Run: secrets.Exec, RunIsolated: secrets.ExecIsolated, Stores: stores}
		if err := r.Login(args[2]); err != nil {
			return err
		}
		fmt.Printf("Bitwarden %s: logged in, locked\n", args[2])
		return nil
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

const bitwardenLoginUsage = "usage: ai-secrets bitwarden login <account>"

const toolAddUsage = "usage: ai-secrets tool add <tool> --vault <vault> [--item <title>] [--profile <profile>] [--account <1password account>] [--from <tool profile>]"

// needsAccount reports whether env holds an op:// reference that names no
// 1Password account.
func needsAccount(env []string) bool {
	for _, kv := range env {
		_, v, _ := strings.Cut(kv, "=")
		if secrets.IsOp(v) && !secrets.HasOpAccount(v) {
			return true
		}
	}
	return false
}

// folderAccount is the onepassword of the current folder's profile, empty
// without one. A configuration that does not load leaves the references as
// they are, with a warning: run also starts MCP servers, which must not
// depend on it.
func folderAccount(home string) string {
	path := config.DefaultPath(home, os.Getenv)
	if !config.Exists(path) {
		return ""
	}
	cfg, err := config.Load(path, home)
	if err == nil {
		cfg.LastUsed = func(dir string) string { return lastused.Get(home, dir) }
		var cwd, account string
		if cwd, err = os.Getwd(); err == nil {
			if account, err = toolshim.FolderOpAccount(cfg, cwd); err == nil {
				return account
			}
		}
	}
	fmt.Fprintf(os.Stderr, "ai-secrets: no 1Password account from the folder's profile: %v\n", err)
	return ""
}

// runToolAdd moves a tool's secret from its config file into 1Password and
// sets up its tool shim (toolshim.Adder).
func runToolAdd(home string, p secretagent.Paths, args []string) error {
	if !shimSupported {
		return errors.New("tool shims are not available on Windows yet")
	}
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return errors.New(toolAddUsage)
	}
	o := toolshim.AddOptions{Tool: args[0]}
	flags := map[string]*string{"--vault": &o.Vault, "--item": &o.Item, "--profile": &o.Profile, "--account": &o.Account, "--from": &o.From}
	for rest := args[1:]; len(rest) > 0; rest = rest[2:] {
		dst, ok := flags[rest[0]]
		if !ok || len(rest) < 2 {
			return errors.New(toolAddUsage)
		}
		*dst = rest[1]
	}
	cfgPath := config.DefaultPath(home, os.Getenv)
	cfg, err := config.Load(cfgPath, home)
	if err != nil {
		return err
	}
	cfg.LastUsed = func(dir string) string { return lastused.Get(home, dir) }
	stores, err := loadStores(home)
	if err != nil {
		return err
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if real, err := filepath.EvalSymlinks(self); err == nil {
		self = real
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	a := &toolshim.Adder{
		Home: home, Dir: cwd, Getenv: os.Getenv, Cfg: cfg, Stores: stores,
		ShimsDir: filepath.Join(home, ".local", "share", "ai-profiles", "tool-shims"),
		Self:     self, PathList: os.Getenv("PATH"),
		CreateItem: createOpItem,
		Get: func(reason string, refs map[string]string, account string) (map[string]string, error) {
			c, err := client(p)
			if err != nil {
				return nil, err
			}
			return c.Get(reason, refs, account)
		},
		Out: os.Stdout,
	}
	return a.Add(o)
}

// createOpItem runs op item create with the template on stdin and returns
// the item's id; op's output, which holds the values, is read, not printed.
func createOpItem(account, vault string, item []byte) (string, error) {
	args := []string{"item", "create", "--vault", vault, "--format", "json"}
	if account != "" {
		args = append(args, "--account", account)
	}
	cmd := exec.Command("op", args...)
	cmd.Stdin = bytes.NewReader(item)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		first, _, _ := strings.Cut(strings.TrimSpace(stderr.String()), "\n")
		return "", fmt.Errorf("op item create: %v: %s", err, first)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(out, &created); err != nil || created.ID == "" {
		return "", errors.New("op item create: no item id in its output")
	}
	return created.ID, nil
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

func runCommand(home string, p secretagent.Paths, args []string) error {
	const use = "usage: ai-secrets run [--reason <reason>] [--account <1password account>] -- <command> [<arguments>]"
	reason, account := "", ""
	for len(args) > 0 && args[0] != "--" {
		switch {
		case args[0] == "--reason" && len(args) > 1:
			reason, args = args[1], args[2:]
		case args[0] == "--account" && len(args) > 1:
			account, args = args[1], args[2:]
		default:
			return errors.New(use)
		}
	}
	if len(args) < 2 {
		return errors.New(use)
	}
	cmd := args[1:]
	if account == "" && needsAccount(os.Environ()) {
		account = folderAccount(home)
	}
	refs := map[string]string{}
	for _, kv := range os.Environ() {
		k, v, _ := strings.Cut(kv, "=")
		if secrets.IsOp(v) || strings.HasPrefix(v, "bw://") {
			refs[k] = secrets.WithOpAccount(v, account)
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
