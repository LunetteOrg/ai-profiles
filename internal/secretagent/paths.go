package secretagent

import "path/filepath"

// Paths are the agent's files: socket, lock and log in ~/.cache/ai-secrets
// (a folder only the user can enter), settings in ~/.config/ai-secrets.
type Paths struct {
	Socket, Lock, Log, Config string
}

func PathsFor(home string) Paths {
	cache := filepath.Join(home, ".cache", "ai-secrets")
	return Paths{
		Socket: filepath.Join(cache, "agent.sock"),
		Lock:   filepath.Join(cache, "agent.lock"),
		Log:    filepath.Join(cache, "agent.log"),
		Config: filepath.Join(home, ".config", "ai-secrets", "config.toml"),
	}
}

// NewClient returns a client that starts the agent (the ai-secrets
// executable bin) when it is not running.
func NewClient(home, bin string) *Client {
	p := PathsFor(home)
	return &Client{Socket: p.Socket, Start: func() error { return Start(bin, p) }}
}
