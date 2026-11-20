package toolshim

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/LunetteOrg/ai-profiles/internal/config"
	"github.com/LunetteOrg/ai-profiles/internal/gitroot"
	"github.com/LunetteOrg/ai-profiles/internal/secrets"
)

// Field is a key of a tool's config file that moves to 1Password.
type Field struct {
	Key    string // in the tool's config file
	Var    string // the variable the tool reads first
	Label  string // the 1Password field
	Secret bool   // removed from the file once in 1Password
}

// Preset is what tool add knows of a tool.
type Preset struct {
	Title    string // of the 1Password item: "<Title> - <tool> CLI <profile>"
	Reason   string
	DenyArgs [][]string
	Fields   []Field
	// ConfigPath is the tool's config file.
	ConfigPath func(home string, getenv func(string) string) string
	// DefaultProfile is the profile of the file used without --from.
	DefaultProfile func(path string) (string, error)
	Read           func(path, profile string) (map[string]string, error)
	// RemoveSecret removes the secret fields of profile and commented-out
	// secrets from the file.
	RemoveSecret func(path, profile string) error
}

// Presets are the tools tool add knows.
var Presets = map[string]*Preset{
	"scw": {
		Title:    "Scaleway",
		Reason:   "Scaleway CLI",
		DenyArgs: [][]string{{"config", "info"}, {"config", "dump"}, {"config", "get", "secret-key"}, {"config", "get", "secret_key"}},
		Fields: []Field{
			{Key: "access_key", Var: "SCW_ACCESS_KEY", Label: "access key"},
			{Key: "secret_key", Var: "SCW_SECRET_KEY", Label: "secret key", Secret: true},
		},
		ConfigPath:     ScwConfigPath,
		DefaultProfile: ScwActiveProfile,
		Read:           ScwRead,
		RemoveSecret:   ScwRemoveSecret,
	},
}

// AddOptions are the arguments of tool add.
type AddOptions struct {
	Tool    string
	Vault   string
	Item    string // title of the new item; from the preset when empty
	Profile string // ai-profile profile; the folder's when empty
	Account string // 1Password account; the profile's onepassword when empty
	From    string // profile of the tool's config file; the preset's default when empty
}

// Adder moves a tool's secret from its config file to 1Password and sets up
// its tool shim.
type Adder struct {
	Home, Dir string
	Getenv    func(string) string
	Cfg       *config.Config
	Stores    *secrets.Stores
	ShimsDir  string
	Self      string // the ai-secrets binary
	PathList  string
	// CreateItem creates a 1Password item from its JSON template in account
	// (op --account; empty lets op pick) and returns its id.
	CreateItem func(account, vault string, item []byte) (string, error)
	// Get asks ai-secrets for the values of refs.
	Get func(reason string, refs map[string]string, account string) (map[string]string, error)
	Out io.Writer
}

// Add runs the steps of tool add, each stopping on failure before the next:
// checks, item in 1Password, read-back comparison, section, link, removal of
// the secret from the tool's file. No step prints or returns a value.
func (a *Adder) Add(o AddOptions) error {
	p := Presets[o.Tool]
	if p == nil {
		return fmt.Errorf("no preset for %s (known: %s): write the section by hand", o.Tool, strings.Join(slices.Sorted(maps.Keys(Presets)), ", "))
	}
	if o.Vault == "" {
		return errors.New("--vault names the 1Password vault of the new item")
	}
	prof, err := a.profile(o.Profile)
	if err != nil {
		return err
	}
	if prof.Tools[o.Tool] != nil {
		return fmt.Errorf("profile %s already has tools.%s in %s", prof.Name, o.Tool, prof.Path)
	}
	accName := o.Account
	if accName == "" {
		accName = prof.OnePassword
	}
	opAccount := ""
	if accName != "" {
		acc := a.Stores.OnePassword[accName]
		if acc == nil {
			return fmt.Errorf("1Password account %q is not in %s ([stores.1password.%s])", accName, secrets.StoresPath(a.Home), accName)
		}
		opAccount = acc.Account
	}
	file := p.ConfigPath(a.Home, a.Getenv)
	from := o.From
	if from == "" {
		if from, err = p.DefaultProfile(file); err != nil {
			return err
		}
	}
	values, err := p.Read(file, from)
	if err != nil {
		return err
	}
	for _, f := range p.Fields {
		if values[f.Key] == "" {
			return fmt.Errorf("profile %s of %s has no %s", from, file, f.Key)
		}
	}
	link := filepath.Join(a.ShimsDir, o.Tool)
	target := LinkTarget(a.Self)
	linkThere := false
	if _, err := os.Lstat(link); err == nil {
		if canonical(link) != canonical(a.Self) {
			return fmt.Errorf("%s exists and is not a link to ai-secrets", link)
		}
		linkThere = true
	}

	// The item, values on stdin.
	title := o.Item
	if title == "" {
		title = fmt.Sprintf("%s - %s CLI %s", p.Title, o.Tool, from)
	}
	item := map[string]any{"title": title, "category": "API_CREDENTIAL"}
	var fields []map[string]any
	for _, f := range p.Fields {
		typ := "STRING"
		if f.Secret {
			typ = "CONCEALED"
		}
		fields = append(fields, map[string]any{"id": strings.ReplaceAll(f.Label, " ", "_"), "label": f.Label, "type": typ, "value": values[f.Key]})
	}
	fields = append(fields, map[string]any{"id": "notesPlain", "label": "notesPlain", "type": "STRING", "purpose": "NOTES",
		"value": fmt.Sprintf("%s profile %s. Read by the ai-secrets tool shim of %s (profile %s).", o.Tool, from, o.Tool, prof.Name)})
	item["fields"] = fields
	tmpl, err := json.Marshal(item)
	if err != nil {
		return err
	}
	id, err := a.CreateItem(opAccount, o.Vault, tmpl)
	if err != nil {
		return fmt.Errorf("creating the 1Password item: %w", err)
	}
	fmt.Fprintf(a.Out, "created %q (%s) in vault %s\n", title, id, o.Vault)

	// The references, read back and compared in this process.
	env := map[string]string{}
	for _, f := range p.Fields {
		ref := fmt.Sprintf("op://%s/%s/%s", o.Vault, id, f.Label)
		if accName != "" && accName != prof.OnePassword {
			ref = secrets.WithOpAccount(ref, accName)
		}
		env[f.Var] = ref
	}
	asked := map[string]string{}
	for k, v := range env {
		asked[k] = secrets.WithOpAccount(v, prof.OnePassword)
	}
	got, err := a.Get(fmt.Sprintf("%s (profile %s): check of the new item", p.Reason, prof.Name), asked, prof.Bitwarden)
	if err != nil {
		return fmt.Errorf("reading the new item back (%s, vault %s): %w", id, o.Vault, err)
	}
	var differ []string
	for _, f := range p.Fields {
		if got[f.Var] != values[f.Key] {
			differ = append(differ, f.Var)
		}
	}
	if len(differ) > 0 {
		return fmt.Errorf("%s read back from item %s (vault %s) differ from %s: nothing else changed; delete the item and check the file", strings.Join(differ, ", "), id, o.Vault, file)
	}
	fmt.Fprintf(a.Out, "checked %s against %s (profile %s)\n", strings.Join(slices.Sorted(maps.Keys(env)), ", "), file, from)

	// The section, the link, then the secret leaves the file.
	tool := config.Tool{Env: env, Reason: p.Reason, DenyArgs: p.DenyArgs}
	if err := config.EditFile(a.Cfg.Path, prof.Path, a.Home, func(src []byte) ([]byte, bool, error) {
		out, err := config.AddToolSection(src, prof.Name, o.Tool, tool)
		return out, err == nil, err
	}); err != nil {
		return err
	}
	fmt.Fprintf(a.Out, "wrote [profiles.%s.tools.%s] in %s\n", prof.Name, o.Tool, prof.Path)
	if !linkThere {
		if err := os.MkdirAll(a.ShimsDir, 0o755); err != nil {
			return err
		}
		if err := os.Symlink(target, link); err != nil {
			return err
		}
		fmt.Fprintf(a.Out, "linked %s → %s\n", link, target)
	}
	if err := p.RemoveSecret(file, from); err != nil {
		return err
	}
	fmt.Fprintf(a.Out, "removed the secret of profile %s, and commented-out secrets, from %s\n", from, file)

	if real, err := FindReal(o.Tool, a.PathList, a.ShimsDir, a.Self); err == nil && strings.Contains(real, string(filepath.Separator)+"mise"+string(filepath.Separator)) {
		fmt.Fprintf(a.Out, "\n%s comes from mise: in interactive shells mise activate puts it before the tool shims.\nAdd to the mise configuration that activates it:\n\n[env]\n_.path = [%q]\n", o.Tool, a.ShimsDir)
	}
	return nil
}

func (a *Adder) profile(name string) (*config.Profile, error) {
	if name != "" {
		p := a.Cfg.Profiles[name]
		if p == nil {
			return nil, fmt.Errorf("no profile %s", name)
		}
		return p, nil
	}
	m, err := a.Cfg.Resolve(gitroot.LookupDir(a.Dir))
	if err != nil {
		return nil, err
	}
	return m.Profile, nil
}

// LinkTarget is where a tool shim points: self, or the same file through
// the "latest" folder next to its version folder (mise's layout), which
// follows updates.
func LinkTarget(self string) string {
	latest := filepath.Join(filepath.Dir(filepath.Dir(self)), "latest", filepath.Base(self))
	if latest != self && canonical(latest) == canonical(self) {
		return latest
	}
	return self
}
