package secrets

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// OnePassword is a 1Password account, named in ai-secrets' configuration
// ([stores.1password.<name>]); Account is what op --account accepts: the
// sign-in address, an email or the account ID.
type OnePassword struct {
	Name    string
	Account string
}

// SplitOpRef separates the account of an op:// reference (?account=<name>)
// from the reference op reads; the other query parameters stay.
func SplitOpRef(ref string) (account, plain string) {
	base, query, ok := strings.Cut(ref, "?")
	if !ok || !IsOp(ref) {
		return "", ref
	}
	var keep []string
	for _, p := range strings.Split(query, "&") {
		if v, ok := strings.CutPrefix(p, "account="); ok {
			account = v
			continue
		}
		keep = append(keep, p)
	}
	if len(keep) == 0 {
		return account, base
	}
	return account, base + "?" + strings.Join(keep, "&")
}

// HasOpAccount reports whether ref names its 1Password account.
func HasOpAccount(ref string) bool {
	_, query, ok := strings.Cut(ref, "?")
	if !ok {
		return false
	}
	for _, p := range strings.Split(query, "&") {
		if strings.HasPrefix(p, "account=") {
			return true
		}
	}
	return false
}

// WithOpAccount gives an op:// reference that names no account the account
// name; other references and those that name one stay as they are.
func WithOpAccount(ref, name string) string {
	if name == "" || !IsOp(ref) || HasOpAccount(ref) {
		return ref
	}
	if strings.Contains(ref, "?") {
		return ref + "&account=" + name
	}
	return ref + "?account=" + name
}

// opRead reads an op:// reference with run, in the account it names.
func (r *Resolver) opRead(run Runner, ref string) (string, error) {
	name, plain := SplitOpRef(ref)
	args := []string{"read"}
	if name != "" {
		var acc *OnePassword
		if r.Stores != nil {
			acc = r.Stores.OnePassword[name]
		}
		if acc == nil {
			return "", &accountError{fmt.Errorf("%s: 1Password account %q is not in %s ([stores.1password.%s])", ref, name, StoresPath(r.Home), name)}
		}
		args = append(args, "--account", acc.Account)
	}
	args = append(args, "--no-newline", plain)
	return run(nil, "op", args...)
}

// parseOnePassword reads [stores.1password]: read and one table per account.
func parseOnePassword(raw map[string]any, path string, s *Stores, read *string) error {
	for k, v := range raw {
		if k == "read" {
			r, ok := v.(string)
			if !ok {
				return fmt.Errorf("%s: stores.1password.read must be a string", path)
			}
			*read = r
			continue
		}
		t, ok := v.(map[string]any)
		if !ok {
			return fmt.Errorf("%s: stores.1password.%s: unknown setting (read, or a [stores.1password.<account>] table)", path, k)
		}
		acc, _ := t["account"].(string)
		if acc == "" {
			return fmt.Errorf("%s: stores.1password.%s: account must name the 1Password account (sign-in address, email or ID, as op --account takes it)", path, k)
		}
		for f := range t {
			if f != "account" {
				return fmt.Errorf("%s: stores.1password.%s: unknown setting %q", path, k, f)
			}
		}
		s.OnePassword[k] = &OnePassword{Name: k, Account: acc}
	}
	return nil
}

// accountError is a reference naming an undeclared account: a configuration
// error, not a failed read.
type accountError struct{ error }

func (e *accountError) Unwrap() error { return e.error }

// opAccounts are the names of the declared 1Password accounts, sorted.
func (r *Resolver) opAccounts() []string {
	if r.Stores == nil {
		return nil
	}
	return slices.Sorted(maps.Keys(r.Stores.OnePassword))
}

// opFailure turns a failed op read into the cause op gave on standard error:
// approval not given, item or field not found, no account chosen, or op's
// own message.
func (r *Resolver) opFailure(ref string, err error) error {
	msg := opMessage(err)
	names := r.opAccounts()
	switch {
	case strings.Contains(msg, "multiple accounts found") || (msg == "" && len(names) > 1 && !HasOpAccount(ref)):
		return fmt.Errorf("%s: no 1Password account for this reference (declared: %s): add ?account=<name>, or --account <name> to ai-secrets run", ref, strings.Join(names, ", "))
	case strings.Contains(msg, "authorization timeout"):
		return fmt.Errorf("%s: 1Password approval not given in time: approve the 1Password notification when it appears, then retry", ref)
	case strings.Contains(msg, "authorization prompt dismissed"):
		return fmt.Errorf("%s: 1Password approval refused (notification dismissed)", ref)
	case strings.Contains(msg, "isn't an item") || strings.Contains(msg, "isn't a field") || strings.Contains(msg, "isn't a vault"):
		return fmt.Errorf("%s: not found in 1Password: %s: check vault, item and field (and the account the reference uses)", ref, msg)
	case msg != "":
		return fmt.Errorf("%s: op read failed: %s", ref, msg)
	}
	return fmt.Errorf("%s: op read failed (1Password approved?): %w", ref, err)
}

// opMessage is op's last error line without its "[ERROR] date time" prefix
// and the reference it repeats.
func opMessage(err error) string {
	var re *RunError
	if !errors.As(err, &re) {
		return ""
	}
	var last string
	for _, l := range strings.Split(re.Stderr, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			last = l
		}
	}
	if rest, ok := strings.CutPrefix(last, "[ERROR] "); ok {
		if f := strings.SplitN(rest, " ", 3); len(f) == 3 {
			last = f[2]
		}
	}
	if rest, ok := strings.CutPrefix(last, "could not read secret '"); ok {
		if _, after, ok := strings.Cut(rest, "': "); ok {
			last = after
		}
	}
	return last
}
