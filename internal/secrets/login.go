package secrets

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// LoginRunner runs a CLI like Runner with input on its standard input; empty
// input leaves it the terminal's, for the CLI's own prompts.
type LoginRunner func(input string, extraEnv []string, name string, args ...string) (string, error)

// ExecLogin is the LoginRunner that runs the real CLI. With input, the CLI's
// standard error is not shown: its prompt echoes what it reads. A failure
// keeps it for the error without the prompt's lines.
func ExecLogin(input string, extraEnv []string, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Env = append(os.Environ(), extraEnv...)
	if input == "" {
		cmd.Stdin = os.Stdin
		return output(cmd)
	}
	cmd.Stdin = strings.NewReader(input)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return string(out), &RunError{Err: err, Stderr: withoutPrompts(stderr.String())}
	}
	return string(out), nil
}

// withoutPrompts drops the lines of a CLI's prompts ("? …"), which repeat
// the answers they read; a prompt redraws itself after a carriage return.
func withoutPrompts(s string) string {
	var keep []string
	for _, line := range strings.Split(s, "\n") {
		if i := strings.LastIndex(line, "\r"); i >= 0 {
			line = line[i+1:]
		}
		if line = strings.TrimSpace(line); line == "" || strings.HasPrefix(line, "? ") {
			continue
		}
		keep = append(keep, line)
	}
	return strings.Join(keep, "\n")
}

// Login logs a Bitwarden account's CLI in from scratch: logout (its local
// data may no longer match the server's), the server, then bw login with the
// master password from 1Password and the two-step code: from 1Password when
// the account has otp, which bw reads at its prompt, else typed on the
// terminal. bw has no option to read the code from the environment, and the
// command line is visible to every process. The session key bw prints is
// dropped and the account left locked.
func (r *Resolver) Login(name string) error {
	var b *Bitwarden
	if r.Stores != nil {
		b = r.Stores.Bitwarden[name]
	}
	if b == nil {
		return fmt.Errorf("Bitwarden account %q is not in %s ([stores.bitwarden.%s])", name, StoresPath(r.Home), name)
	}
	if b.Email == "" {
		return fmt.Errorf("%s: stores.bitwarden.%s has no email: add the account's email to log in", StoresPath(r.Home), name)
	}
	if !IsOp(b.Password) {
		return fmt.Errorf("%s: stores.bitwarden.%s: the login needs the master password in 1Password (password = \"op://…\")", StoresPath(r.Home), name)
	}
	// Not logged in is not an error here: logout only clears old data.
	r.Run(b.Env(), "bw", "logout")
	if b.Server != "" {
		if _, err := r.Run(b.Env(), "bw", "config", "server", b.Server); err != nil {
			return fmt.Errorf("Bitwarden %s: setting the server %s: %v", name, b.Server, err)
		}
	}
	read := r.isolated()
	pw, err := r.opRead(read, b.Password)
	if err != nil || pw == "" {
		return fmt.Errorf("Bitwarden %s master password from 1Password: %v", name, err)
	}
	args := []string{"login", b.Email, "--passwordenv", "BW_PASSWORD"}
	input := ""
	if b.OTP != "" {
		code, err := r.opRead(read, b.OTP)
		if err != nil || code == "" {
			return fmt.Errorf("Bitwarden %s two-step code from 1Password: %v", name, err)
		}
		args = append(args, "--method", "0") // authenticator app: the code 1Password computes
		input = strings.TrimSpace(code) + "\n"
	}
	args = append(args, "--raw")
	login := r.RunLogin
	if login == nil {
		login = ExecLogin
	}
	_, err = login(input, append(b.Env(), "BW_PASSWORD="+pw), "bw", args...)
	pw, input = "", ""
	if err != nil {
		var re *RunError
		if errors.As(err, &re) && strings.TrimSpace(re.Stderr) != "" {
			return fmt.Errorf("Bitwarden %s: login failed: %s", name, strings.TrimSpace(re.Stderr))
		}
		return fmt.Errorf("Bitwarden %s: login failed: %v", name, err)
	}
	r.Run(b.Env(), "bw", "lock")
	return nil
}
