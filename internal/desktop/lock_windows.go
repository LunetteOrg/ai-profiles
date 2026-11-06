//go:build windows

package desktop

// lock does nothing: Claude Desktop per profile runs only on macOS.
func lock(Instance) (func(), error) { return func() {}, nil }
