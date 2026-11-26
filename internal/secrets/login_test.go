package secrets

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

// fakeLogin records a Bitwarden login: the master password is "principale",
// the two-step code "123456", the session key printed by bw "sessione".
type fakeLogin struct {
	calls    []call
	inputs   []string
	order    []string
	password string // the master password in 1Password, "principale" when empty
}

func (f *fakeLogin) run(env []string, name string, args ...string) (string, error) {
	f.calls = append(f.calls, call{env, name, args})
	f.order = append(f.order, name+" "+args[0])
	if name == "bw" && args[0] == "logout" {
		return "", errors.New("You are not logged in.")
	}
	return "", nil
}

func (f *fakeLogin) runIsolated(env []string, name string, args ...string) (string, error) {
	f.calls = append(f.calls, call{env, name, args})
	f.order = append(f.order, name+" "+args[0])
	if name == "op" {
		if strings.Contains(args[len(args)-1], "attribute=otp") {
			return "123456", nil
		}
		if f.password != "" {
			return f.password, nil
		}
		return "principale", nil
	}
	return "", nil
}

func (f *fakeLogin) runLogin(input string, env []string, name string, args ...string) (string, error) {
	f.calls = append(f.calls, call{env, name, args})
	f.inputs = append(f.inputs, input)
	f.order = append(f.order, name+" "+args[0])
	if !contains(env, "BW_PASSWORD=principale") {
		return "", errors.New("password sbagliata")
	}
	return "sessione\n", nil
}

func (f *fakeLogin) find(name, first string) *call {
	for i := range f.calls {
		if f.calls[i].name == name && f.calls[i].args[0] == first {
			return &f.calls[i]
		}
	}
	return nil
}

func newLoginResolver(f *fakeLogin, b *Bitwarden) *Resolver {
	return &Resolver{
		Getenv:      func(string) string { return "" },
		Run:         f.run,
		RunIsolated: f.runIsolated,
		RunLogin:    f.runLogin,
		Stores:      &Stores{TTL: DefaultTTL, OpIsolated: true, Bitwarden: map[string]*Bitwarden{b.Name: b}},
	}
}

func TestBitwardenLoginWithCodeFrom1Password(t *testing.T) {
	f := &fakeLogin{}
	r := newLoginResolver(f, &Bitwarden{
		Name: "client-e", DataDir: "/dati/client-e", Server: "https://vault.bitwarden.eu",
		Unlock: "password", Password: "op://Private/bw/password", Email: "me@client-e.example",
		OTP: "op://Private/bw/one-time password?attribute=otp", LockAfterRead: true,
	})
	if err := r.Login("client-e"); err != nil {
		t.Fatal(err)
	}
	want := []string{"bw logout", "bw config", "op read", "op read", "bw login", "bw lock"}
	if !slices.Equal(f.order, want) {
		t.Errorf("order = %v, want %v", f.order, want)
	}
	if c := f.find("bw", "config"); c == nil || !slices.Equal(c.args, []string{"config", "server", "https://vault.bitwarden.eu"}) {
		t.Errorf("server set before the login: %+v", c)
	}
	login := f.find("bw", "login")
	if login == nil || !slices.Equal(login.args, []string{"login", "me@client-e.example", "--passwordenv", "BW_PASSWORD", "--method", "0", "--raw"}) {
		t.Fatalf("login = %+v", login)
	}
	if f.inputs[0] != "123456\n" {
		t.Errorf("the code goes to bw's prompt on standard input, got %q", f.inputs[0])
	}
	for _, c := range f.calls {
		if !contains(c.env, "BITWARDENCLI_APPDATA_DIR=/dati/client-e") && c.name == "bw" {
			t.Errorf("%s %v runs outside the account's data folder", c.name, c.args)
		}
		for _, a := range c.args {
			if a == "123456" || a == "principale" || a == "sessione" {
				t.Errorf("a secret on the command line of %s %v", c.name, c.args)
			}
		}
		if c.name != "bw" || c.args[0] != "login" {
			for _, e := range c.env {
				if strings.HasPrefix(e, "BW_PASSWORD=") {
					t.Errorf("the master password reaches %s %v", c.name, c.args)
				}
			}
		}
	}
}

func TestBitwardenLoginAsksTheCodeOnTheTerminal(t *testing.T) {
	f := &fakeLogin{}
	r := newLoginResolver(f, &Bitwarden{
		Name: "client-e", DataDir: "/dati/client-e", Unlock: "password",
		Password: "op://Private/bw/password", Email: "me@client-e.example", LockAfterRead: true,
	})
	if err := r.Login("client-e"); err != nil {
		t.Fatal(err)
	}
	if f.find("bw", "config") != nil {
		t.Errorf("no server configured, none set")
	}
	login := f.find("bw", "login")
	if login == nil || slices.Contains(login.args, "--method") || f.inputs[0] != "" {
		t.Errorf("without otp bw asks on the terminal: %+v input %q", login, f.inputs)
	}
}

func TestBitwardenLoginErrors(t *testing.T) {
	f := &fakeLogin{}
	r := newLoginResolver(f, &Bitwarden{Name: "client-e", Unlock: "password", Password: "op://Private/bw/password"})
	if err := r.Login("client-e"); err == nil || !strings.Contains(err.Error(), "email") {
		t.Errorf("an account without email: %v", err)
	}
	if err := r.Login("nessuno"); err == nil || !strings.Contains(err.Error(), "stores.bitwarden.nessuno") {
		t.Errorf("an unknown account: %v", err)
	}
	if len(f.calls) != 0 {
		t.Errorf("nothing runs before the configuration is checked: %v", f.order)
	}
	r.Stores.Bitwarden["client-e"].Email = "me@client-e.example"
	r.Stores.Bitwarden["client-e"].Password = "op://Private/sbagliata/password"
	f.password = "altra"
	err := r.Login("client-e")
	if err == nil || strings.Contains(err.Error(), "altra") {
		t.Errorf("a login refused fails without showing the password: %v", err)
	}
}

func TestWithoutPrompts(t *testing.T) {
	in := "? Two-step login code: \x1b[23D\x1b[2K\x1b[G? Two-step login code: 3\x1b[24D\r? Two-step login code: 394146\nInvalid code.\n"
	if got := withoutPrompts(in); got != "Invalid code." {
		t.Errorf("withoutPrompts = %q", got)
	}
}
