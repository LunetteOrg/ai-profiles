// Package desktop runs Claude Desktop once per profile: a copy of Claude.app
// started with the profile's --user-data-dir and CLAUDE_CONFIG_DIR, and a
// launcher in ~/Applications that opens it through ai-profile desktop.
//
// The copy is realigned to Claude.app every time it is opened, because Claude
// keeps updating itself: the copy of /Applications/Claude.app falls behind,
// and a copy updated by Claude's own updater restarts without the profile and
// without its icon.
package desktop

import (
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/LunetteOrg/ai-profiles/internal/launch"
)

// DefaultSource is the Claude.app the copies are made from.
const DefaultSource = "/Applications/Claude.app"

// BundlePrefix starts the bundle identifier of every launcher ai-profile writes.
const BundlePrefix = "io.github.ilmeskio.ai-profile.desktop."

// Instance is the Claude Desktop of one profile.
type Instance struct {
	Name   string // profile
	Home   string
	Hue    float64 // icon tint, in degrees
	Source string  // Claude.app to copy, DefaultSource when empty
}

func title(name string) string {
	r, size := utf8.DecodeRuneInString(name)
	return string(unicode.ToUpper(r)) + name[size:]
}

// Label is the app name, "Claude Client-d".
func (in Instance) Label() string { return "Claude " + title(in.Name) }

// Launcher is ~/Applications/Claude <Profile>.app.
func (in Instance) Launcher() string {
	return filepath.Join(in.Home, "Applications", in.Label()+".app")
}

func (in Instance) dir() string {
	return filepath.Join(in.Home, "Library", "Application Support", "ai-profile", "desktop")
}

// Copy is the profile's copy of Claude.app.
func (in Instance) Copy() string { return filepath.Join(in.dir(), in.Label()+".app") }

// DataDir is the Electron data folder: login, chats, connectors, extensions.
func (in Instance) DataDir() string {
	return filepath.Join(in.Home, "Library", "Application Support", "Claude-"+title(in.Name))
}

// ConfigDir is the Claude Code folder the Code tab uses.
func (in Instance) ConfigDir() string { return launch.ProfileDir(in.Home, "claude", in.Name) }

func (in Instance) source() string {
	if in.Source == "" {
		return DefaultSource
	}
	return in.Source
}

// OpenArgs are the arguments of open(1) that start the copy on the profile.
func (in Instance) OpenArgs() []string {
	return []string{"-a", in.Copy(), "--env", "CLAUDE_CONFIG_DIR=" + in.ConfigDir(), "--args", "--user-data-dir=" + in.DataDir()}
}

// Process is a running process and its command line.
type Process struct {
	PID     int
	Command string
}

// System is what the package does outside its own folders; tests replace it.
type System interface {
	// Version is the app's CFBundleShortVersionString, "" when unreadable.
	Version(app string) string
	Clone(src, dst string) error
	// Tint gives app the icon of iconFrom with its hue rotated.
	Tint(app, iconFrom string, hue float64) error
	Processes() ([]Process, error)
	// Quit asks the processes to quit and waits; false if one is still running.
	Quit(pids []int) bool
	// Ask shows a dialog and reports whether yes was chosen.
	Ask(title, msg, yes, no string) bool
	Open(args []string) error
	// Register tells Launch Services about a new app at that path.
	Register(app string)
	// Applet compiles an AppleScript into an app, sets keys in its
	// Info.plist and signs it.
	Applet(app, source string, keys map[string]string) error
}

// running splits the copy's main processes into those started on the
// profile and those started without it (by Claude's updater, a Dock tile or
// Spotlight). Helpers live under Contents/Frameworks and are left out.
func (in Instance) running(procs []Process) (mine, without []int) {
	main := in.Copy() + "/Contents/MacOS/"
	flag := " --user-data-dir=" + in.DataDir()
	for _, p := range procs {
		if !strings.HasPrefix(p.Command, main) {
			continue
		}
		// Followed by a space or the end, so Claude-Work never matches Claude-Work2.
		if i := strings.Index(p.Command, flag); i >= 0 {
			if rest := p.Command[i+len(flag):]; rest == "" || rest[0] == ' ' {
				mine = append(mine, p.PID)
				continue
			}
		}
		without = append(without, p.PID)
	}
	return mine, without
}

func tinted(app string) bool { return exists(filepath.Join(app, "Icon\r")) }

// Open opens the profile's Claude Desktop: it quits, after asking, a copy
// running without the profile, realigns the copy to Claude.app when it is
// not running, puts back its icon and starts it. It returns what it did on
// the way.
func Open(in Instance, sys System) ([]string, error) {
	var did []string
	unlock, err := lock(in)
	if err != nil {
		return nil, err
	}
	defer unlock()

	procs, err := sys.Processes()
	if err != nil {
		return nil, err
	}
	mine, without := in.running(procs)
	if len(without) > 0 {
		msg := in.Label() + " is open without its profile.\n\n" +
			"It was started by a Claude update, a Dock icon of the open window or Spotlight, " +
			"so it shows the data and account of the default Claude.\n\n" +
			"Quit it and reopen it on profile " + in.Name + "?"
		if !sys.Ask(in.Label(), msg, "Quit and Reopen", "Leave It Open") {
			return append(did, "left open without the profile, as asked"), nil
		}
		if !sys.Quit(without) {
			return did, fmt.Errorf("%s did not quit: quit it from its Claude menu and open it again", in.Label())
		}
		did = append(did, "quit the copy open without the profile")
	}
	if len(mine) == 0 {
		msg, err := refresh(in, sys)
		if msg != "" {
			did = append(did, msg)
		}
		if err != nil {
			if sys.Version(in.Copy()) == "" {
				return did, err
			}
			did = append(did, fmt.Sprintf("could not update the copy (%v): opening it as it is", err))
		}
	}
	if !tinted(in.Copy()) {
		if err := sys.Tint(in.Copy(), in.Copy(), in.Hue); err != nil {
			did = append(did, fmt.Sprintf("could not tint the icon: %v", err))
		} else {
			did = append(did, "put back the profile's icon")
		}
	}
	return did, sys.Open(in.OpenArgs())
}

// refresh clears what an interrupted rebuild left behind and rebuilds the
// copy from Claude.app when it is missing, older, or the same version without
// the profile's icon: macOS keeps a launcher from writing into an app that
// has run, so the icon goes on a new copy before it first runs. The new copy
// is built beside the old one and swapped in only once complete.
func refresh(in Instance, sys System) (string, error) {
	cp := in.Copy()
	tmp, old := cp+".refreshing", cp+".previous"
	if err := os.RemoveAll(tmp); err != nil {
		return "", err
	}
	if !exists(cp) && exists(old) {
		// Stopped between the two renames: the old copy is still whole.
		if err := os.Rename(old, cp); err != nil {
			return "", err
		}
	}
	if err := os.RemoveAll(old); err != nil {
		return "", err
	}
	want := sys.Version(in.source())
	if want == "" {
		return "", fmt.Errorf("cannot read the version of %s", in.source())
	}
	have := sys.Version(cp)
	if have != "" && (compare(have, want) > 0 || compare(have, want) == 0 && tinted(cp)) {
		return "", nil
	}
	if err := os.MkdirAll(in.dir(), 0o755); err != nil {
		return "", err
	}
	fail := func(err error) (string, error) {
		os.RemoveAll(tmp)
		return "", err
	}
	if err := sys.Clone(in.source(), tmp); err != nil {
		return fail(fmt.Errorf("copying %s: %w", in.source(), err))
	}
	if got := sys.Version(tmp); got != want {
		return fail(fmt.Errorf("the copy came out as %q, not %s", got, want))
	}
	if err := sys.Tint(tmp, tmp, in.Hue); err != nil {
		return fail(fmt.Errorf("tinting the icon: %w", err))
	}
	if exists(cp) {
		if err := os.Rename(cp, old); err != nil {
			return fail(err)
		}
	}
	if err := os.Rename(tmp, cp); err != nil {
		os.Rename(old, cp)
		return fail(err)
	}
	os.RemoveAll(old)
	sys.Register(cp)
	switch {
	case have == "":
		return "made the copy of Claude " + want, nil
	case have == want:
		return "made the copy again to put back the profile's icon", nil
	}
	return fmt.Sprintf("updated the copy from %s to %s", have, want), nil
}

// launcherKey records in the launcher's Info.plist how it was written, so
// install rewrites it when ai-profile moves or the launcher's form changes.
const launcherKey = "AIProfileLauncher"

func launcherStamp(aiProfile string) string { return "applet " + aiProfile }

// Install writes the profile's launcher, an AppleScript applet that runs
// "ai-profile desktop <profile>" with the tinted icon of Claude.app, and
// prepares the copy as Open would, unless it is open. It is an applet because
// Spotlight lists only apps with a real executable. A launcher ai-profile did
// not write goes to the Trash. It returns what it did, nothing when all was
// in order.
func Install(in Instance, sys System, aiProfile string) ([]string, error) {
	var did []string
	app := in.Launcher()
	stamp := launcherStamp(aiProfile)
	if !Ours(in) || plistString(app, launcherKey) != stamp || !tinted(app) {
		switch {
		case Ours(in):
			if err := os.RemoveAll(app); err != nil {
				return did, err
			}
		case exists(app):
			trash := filepath.Join(in.Home, ".Trash", in.Label()+".app-"+time.Now().Format("20060102-150405"))
			if err := os.MkdirAll(filepath.Dir(trash), 0o755); err != nil {
				return did, err
			}
			if err := os.Rename(app, trash); err != nil {
				return did, err
			}
			did = append(did, "moved the launcher ai-profile did not write to the Trash")
		}
		if err := os.MkdirAll(filepath.Dir(app), 0o755); err != nil {
			return did, err
		}
		keys := map[string]string{
			"CFBundleIdentifier": BundlePrefix + in.Name,
			"CFBundleName":       in.Label(),
			launcherKey:          stamp,
		}
		if err := sys.Applet(app, launcherScript(in, aiProfile), keys); err != nil {
			return did, fmt.Errorf("writing %s: %w", app, err)
		}
		if err := sys.Tint(app, in.source(), in.Hue); err != nil {
			return did, fmt.Errorf("tinting %s: %w", app, err)
		}
		sys.Register(app)
		did = append(did, "wrote the launcher")
	}
	msg, err := Repair(in, sys)
	if msg != "" {
		did = append(did, msg)
	}
	return did, err
}

// Ours reports whether the launcher was written by ai-profile.
func Ours(in Instance) bool {
	return plistString(in.Launcher(), "CFBundleIdentifier") == BundlePrefix+in.Name
}

// plistString reads a string key of an app's XML Info.plist.
func plistString(app, key string) string {
	b, err := os.ReadFile(filepath.Join(app, "Contents", "Info.plist"))
	if err != nil {
		return ""
	}
	re := regexp.MustCompile(`<key>` + regexp.QuoteMeta(key) + `</key>\s*<string>([^<]*)</string>`)
	if m := re.FindSubmatch(b); m != nil {
		return html.UnescapeString(string(m[1]))
	}
	return ""
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

func appleScriptString(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// launcherScript runs ai-profile and waits for it, so nothing is cut short
// when the launcher exits, and shows its error when it fails.
func launcherScript(in Instance, aiProfile string) string {
	cmd := shellQuote(aiProfile) + " desktop " + shellQuote(in.Name) + " 2>&1"
	return "-- Written by ai-profile desktop install: opens Claude Desktop on profile " + in.Name + ".\n" +
		"try\n" +
		"\tdo shell script " + appleScriptString(cmd) + "\n" +
		"on error msg\n" +
		"\tdisplay alert " + appleScriptString(in.Label()) + " message msg as critical\n" +
		"end try\n"
}

// LauncherState says whether the launcher is in place.
type LauncherState int

const (
	LauncherMissing LauncherState = iota
	LauncherForeign               // not written by ai-profile, e.g. claude-multiprofile's
	LauncherOK
)

// State is what doctor reports about a profile's Claude Desktop.
type State struct {
	Launcher LauncherState
	// Copy and Source are the versions of the copy ("" when missing) and of Claude.app.
	Copy, Source string
	Tinted       bool
	// Leftovers are folders of an interrupted rebuild.
	Leftovers []string
	// Running is true when the copy is open; WithoutProfile lists the main
	// processes started without the profile's data folder.
	Running        bool
	WithoutProfile []int
}

// Behind reports whether the copy is older than Claude.app.
func (s State) Behind() bool {
	return s.Copy != "" && s.Source != "" && compare(s.Copy, s.Source) < 0
}

func Check(in Instance, sys System) State {
	s := State{Source: sys.Version(in.source()), Copy: sys.Version(in.Copy()), Tinted: tinted(in.Copy())}
	switch {
	case Ours(in):
		s.Launcher = LauncherOK
	case exists(in.Launcher()):
		s.Launcher = LauncherForeign
	}
	for _, p := range []string{in.Copy() + ".refreshing", in.Copy() + ".previous"} {
		if exists(p) {
			s.Leftovers = append(s.Leftovers, p)
		}
	}
	if procs, err := sys.Processes(); err == nil {
		mine, without := in.running(procs)
		s.Running = len(mine)+len(without) > 0
		s.WithoutProfile = without
	}
	return s
}

// Repair does what Open does before starting the copy: it realigns the copy
// to Claude.app, unless it is open, and puts back its icon.
func Repair(in Instance, sys System) (string, error) {
	var did []string
	if !Check(in, sys).Running {
		msg, err := refresh(in, sys)
		if msg != "" {
			did = append(did, msg)
		}
		if err != nil {
			return strings.Join(did, "; "), err
		}
	}
	if exists(in.Copy()) && !tinted(in.Copy()) {
		if err := sys.Tint(in.Copy(), in.Copy(), in.Hue); err != nil {
			return strings.Join(did, "; "), err
		}
		did = append(did, "put back the profile's icon")
	}
	return strings.Join(did, "; "), nil
}

func compare(a, b string) int {
	x, y := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < max(len(x), len(y)); i++ {
		var p, r int
		if i < len(x) {
			p, _ = strconv.Atoi(x[i])
		}
		if i < len(y) {
			r, _ = strconv.Atoi(y[i])
		}
		if p != r {
			if p < r {
				return -1
			}
			return 1
		}
	}
	return 0
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}
