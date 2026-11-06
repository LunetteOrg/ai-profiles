package desktop

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// fakeSystem records what is done to processes and apps; an app's version
// is a file inside it, so renames and removals carry it along.
type fakeSystem struct {
	procs    []Process
	answer   bool
	quits    bool
	cloneErr error
	asked    []string
	quit     []int
	opened   [][]string
	tinted   []string
	clones   int
}

func (f *fakeSystem) Version(app string) string {
	b, _ := os.ReadFile(filepath.Join(app, "Contents", "version"))
	return string(b)
}

func (f *fakeSystem) Clone(src, dst string) error {
	if f.cloneErr != nil {
		return f.cloneErr
	}
	f.clones++
	return writeApp(dst, f.Version(src))
}

func (f *fakeSystem) Tint(app, iconFrom string, hue float64) error {
	f.tinted = append(f.tinted, app)
	return os.WriteFile(filepath.Join(app, "Icon\r"), nil, 0o644)
}

func (f *fakeSystem) Processes() ([]Process, error) { return f.procs, nil }

func (f *fakeSystem) Quit(pids []int) bool {
	f.quit = append(f.quit, pids...)
	if f.quits {
		f.procs = nil
	}
	return f.quits
}

func (f *fakeSystem) Ask(title, msg, yes, no string) bool {
	f.asked = append(f.asked, msg)
	return f.answer
}

func (f *fakeSystem) Open(args []string) error {
	f.opened = append(f.opened, args)
	return nil
}

func (f *fakeSystem) Register(string) {}

// Applet writes the Info.plist keys and keeps the AppleScript source as it is.
func (f *fakeSystem) Applet(app, source string, keys map[string]string) error {
	return fakeApplet(app, source, keys)
}

func fakeApplet(app, source string, keys map[string]string) error {
	if err := os.MkdirAll(filepath.Join(app, "Contents", "Resources", "Scripts"), 0o755); err != nil {
		return err
	}
	plist := "<plist><dict>\n"
	for k, v := range keys {
		plist += "\t<key>" + k + "</key>\n\t<string>" + v + "</string>\n"
	}
	if err := os.WriteFile(filepath.Join(app, "Contents", "Info.plist"), []byte(plist+"</dict></plist>\n"), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(app, "Contents", "Resources", "Scripts", "main.applescript"), []byte(source), 0o644)
}

func writeApp(app, version string) error {
	if err := os.MkdirAll(filepath.Join(app, "Contents"), 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(app, "Contents", "version"), []byte(version), 0o644)
}

func setup(t *testing.T) (Instance, *fakeSystem) {
	t.Helper()
	home := t.TempDir()
	in := Instance{Name: "client-d", Home: home, Hue: 330, Source: filepath.Join(home, "Claude.app")}
	if err := writeApp(in.Source, "3.6556.2"); err != nil {
		t.Fatal(err)
	}
	return in, &fakeSystem{quits: true}
}

func makeCopy(t *testing.T, in Instance, version string, tinted bool) {
	t.Helper()
	if err := writeApp(in.Copy(), version); err != nil {
		t.Fatal(err)
	}
	if tinted {
		os.WriteFile(filepath.Join(in.Copy(), "Icon\r"), nil, 0o644)
	}
}

func TestPaths(t *testing.T) {
	in := Instance{Name: "client-d", Home: "/Users/io"}
	for got, want := range map[string]string{
		in.Label():     "Claude Client-d",
		in.Launcher():  "/Users/io/Applications/Claude Client-d.app",
		in.Copy():      "/Users/io/Library/Application Support/ai-profile/desktop/Claude Client-d.app",
		in.DataDir():   "/Users/io/Library/Application Support/Claude-Client-d",
		in.ConfigDir(): "/Users/io/.claude-client-d",
	} {
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
	want := []string{"-a", in.Copy(), "--env", "CLAUDE_CONFIG_DIR=/Users/io/.claude-client-d", "--args", "--user-data-dir=/Users/io/Library/Application Support/Claude-Client-d"}
	if got := in.OpenArgs(); !slices.Equal(got, want) {
		t.Errorf("OpenArgs = %q", got)
	}
}

func TestOpenBuildsMissingCopy(t *testing.T) {
	in, sys := setup(t)
	if _, err := Open(in, sys); err != nil {
		t.Fatal(err)
	}
	if sys.Version(in.Copy()) != "3.6556.2" || !exists(filepath.Join(in.Copy(), "Icon\r")) {
		t.Errorf("copy %q, tinted %v", sys.Version(in.Copy()), sys.tinted)
	}
	if len(sys.opened) != 1 || !slices.Equal(sys.opened[0], in.OpenArgs()) {
		t.Errorf("opened %q", sys.opened)
	}
}

func TestOpenRefreshesOldCopyAndClearsLeftovers(t *testing.T) {
	in, sys := setup(t)
	makeCopy(t, in, "2.31226.0", true)
	os.MkdirAll(in.Copy()+".refreshing", 0o755)
	os.MkdirAll(in.Copy()+".previous", 0o755)
	did, err := Open(in, sys)
	if err != nil {
		t.Fatal(err)
	}
	if sys.Version(in.Copy()) != "3.6556.2" || exists(in.Copy()+".refreshing") || exists(in.Copy()+".previous") {
		t.Errorf("copy %q, leftovers still there", sys.Version(in.Copy()))
	}
	if !slices.ContainsFunc(did, func(s string) bool { return strings.Contains(s, "2.31226.0 to 3.6556.2") }) {
		t.Errorf("did = %q", did)
	}
}

func TestOpenRestoresCopyLeftBetweenRenames(t *testing.T) {
	in, sys := setup(t)
	// The swap stopped after moving the copy aside: the old copy comes back
	// instead of a new clone.
	writeApp(in.Copy()+".previous", "3.6556.2")
	os.WriteFile(filepath.Join(in.Copy()+".previous", "Icon\r"), nil, 0o644)
	if _, err := Open(in, sys); err != nil {
		t.Fatal(err)
	}
	if sys.Version(in.Copy()) != "3.6556.2" || exists(in.Copy()+".previous") || sys.clones != 0 {
		t.Errorf("copy %q, clones %d", sys.Version(in.Copy()), sys.clones)
	}
}

func TestOpenKeepsCurrentCopy(t *testing.T) {
	in, sys := setup(t)
	makeCopy(t, in, "3.6556.2", true)
	if _, err := Open(in, sys); err != nil {
		t.Fatal(err)
	}
	if sys.clones != 0 || len(sys.tinted) != 0 || len(sys.opened) != 1 {
		t.Errorf("clones %d, tinted %v, opened %d", sys.clones, sys.tinted, len(sys.opened))
	}
}

func TestOpenRebuildsCopyWithoutIcon(t *testing.T) {
	in, sys := setup(t)
	// macOS keeps a launcher from writing into an app that has run, so a
	// copy that lost its icon is rebuilt and tinted before it first runs.
	makeCopy(t, in, "3.6556.2", false)
	if _, err := Open(in, sys); err != nil {
		t.Fatal(err)
	}
	if sys.clones != 1 || !slices.Equal(sys.tinted, []string{in.Copy() + ".refreshing"}) || !exists(filepath.Join(in.Copy(), "Icon\r")) {
		t.Errorf("clones %d, tinted %v", sys.clones, sys.tinted)
	}
}

func TestOpenTintsCopyUpdatedByClaude(t *testing.T) {
	in, sys := setup(t)
	// Claude's own updater replaced the copy: newer than the source and
	// without the profile's icon.
	makeCopy(t, in, "3.7000.0", false)
	if _, err := Open(in, sys); err != nil {
		t.Fatal(err)
	}
	if sys.clones != 0 || !slices.Equal(sys.tinted, []string{in.Copy()}) {
		t.Errorf("clones %d, tinted %v", sys.clones, sys.tinted)
	}
}

func TestOpenFailedRefreshKeepsOldCopy(t *testing.T) {
	in, sys := setup(t)
	makeCopy(t, in, "2.31226.0", true)
	sys.cloneErr = errors.New("disk full")
	did, err := Open(in, sys)
	if err != nil {
		t.Fatal(err)
	}
	if sys.Version(in.Copy()) != "2.31226.0" || exists(in.Copy()+".refreshing") || len(sys.opened) != 1 {
		t.Errorf("copy %q, opened %d", sys.Version(in.Copy()), len(sys.opened))
	}
	if !slices.ContainsFunc(did, func(s string) bool { return strings.Contains(s, "disk full") }) {
		t.Errorf("did = %q", did)
	}
	os.RemoveAll(in.Copy())
	if _, err := Open(in, sys); err == nil {
		t.Errorf("no copy and no clone: expected an error")
	}
}

func TestOpenFocusesRunningCopy(t *testing.T) {
	in, sys := setup(t)
	makeCopy(t, in, "2.31226.0", true)
	sys.procs = []Process{{PID: 7, Command: in.Copy() + "/Contents/MacOS/Claude --user-data-dir=" + in.DataDir()}}
	if _, err := Open(in, sys); err != nil {
		t.Fatal(err)
	}
	// A running copy is not replaced under the app.
	if sys.clones != 0 || len(sys.asked) != 0 || len(sys.opened) != 1 {
		t.Errorf("clones %d, asked %q, opened %d", sys.clones, sys.asked, len(sys.opened))
	}
}

func TestOpenRestartsCopyWithoutProfile(t *testing.T) {
	in, sys := setup(t)
	makeCopy(t, in, "3.6556.2", true)
	sys.procs = []Process{
		{PID: 7, Command: in.Copy() + "/Contents/MacOS/Claude"},
		// Helpers run from Contents/Frameworks and do not count.
		{PID: 8, Command: in.Copy() + "/Contents/Frameworks/Claude Helper.app/Contents/MacOS/Claude Helper"},
	}
	sys.answer = true
	if _, err := Open(in, sys); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(sys.quit, []int{7}) || len(sys.opened) != 1 {
		t.Errorf("quit %v, opened %d", sys.quit, len(sys.opened))
	}

	in, sys = setup(t)
	makeCopy(t, in, "3.6556.2", true)
	sys.procs = []Process{{PID: 7, Command: in.Copy() + "/Contents/MacOS/Claude --user-data-dir=" + in.DataDir() + "2"}}
	sys.answer = false
	if _, err := Open(in, sys); err != nil {
		t.Fatal(err)
	}
	if len(sys.asked) != 1 || len(sys.quit) != 0 || len(sys.opened) != 0 {
		t.Errorf("another data folder counts as no profile: asked %d, quit %v, opened %d", len(sys.asked), sys.quit, len(sys.opened))
	}

	in, sys = setup(t)
	makeCopy(t, in, "3.6556.2", true)
	sys.procs = []Process{{PID: 7, Command: in.Copy() + "/Contents/MacOS/Claude"}}
	sys.answer, sys.quits = true, false
	if _, err := Open(in, sys); err == nil {
		t.Errorf("a copy that does not quit is an error")
	}
}

func TestInstall(t *testing.T) {
	in, sys := setup(t)
	did, err := Install(in, sys, "/x/shims/ai-profile")
	if err != nil || len(did) == 0 {
		t.Fatalf("did %q, %v", did, err)
	}
	// An AppleScript applet, as Spotlight lists only apps with a real
	// executable; do shell script without & waits for ai-profile.
	script, _ := os.ReadFile(filepath.Join(in.Launcher(), "Contents", "Resources", "Scripts", "main.applescript"))
	if !strings.Contains(string(script), `do shell script "'/x/shims/ai-profile' desktop 'client-d' 2>&1"`) || !strings.Contains(string(script), `display alert "Claude Client-d"`) {
		t.Errorf("script:\n%s", script)
	}
	if !slices.Contains(sys.tinted, in.Launcher()) {
		t.Errorf("tinted %v", sys.tinted)
	}
	if !Ours(in) {
		t.Errorf("the launcher just written is not recognized")
	}
	// The copy is ready before the first open.
	if sys.Version(in.Copy()) != "3.6556.2" || !exists(filepath.Join(in.Copy(), "Icon\r")) || len(sys.opened) != 0 {
		t.Errorf("copy %q, opened %d", sys.Version(in.Copy()), len(sys.opened))
	}
	if did, err := Install(in, sys, "/x/shims/ai-profile"); err != nil || len(did) > 0 {
		t.Errorf("second install: did %q, %v", did, err)
	}
	if did, _ := Install(in, sys, "/y/ai-profile"); len(did) == 0 {
		t.Errorf("a new ai-profile path rewrites the launcher")
	}
	// A launcher of an earlier ai-profile: its bundle identifier, no record
	// of how it was written.
	fakeApplet(in.Launcher(), "sh", map[string]string{"CFBundleIdentifier": BundlePrefix + "client-d"})
	if did, _ := Install(in, sys, "/y/ai-profile"); len(did) == 0 {
		t.Errorf("an earlier launcher is rewritten")
	}
}

func TestAppleScriptString(t *testing.T) {
	if got := appleScriptString(`a "b" \c`); got != `"a \"b\" \\c"` {
		t.Errorf("got %s", got)
	}
}

func TestInstallSetsAsideForeignLauncher(t *testing.T) {
	in, sys := setup(t)
	scripts := filepath.Join(in.Launcher(), "Contents", "Resources", "Scripts")
	os.MkdirAll(scripts, 0o755)
	os.WriteFile(filepath.Join(scripts, "main.scpt"), []byte("applet"), 0o644)
	if Ours(in) {
		t.Fatalf("a claude-multiprofile launcher is not ours")
	}
	if _, err := Install(in, sys, "/x/ai-profile"); err != nil {
		t.Fatal(err)
	}
	if !Ours(in) || exists(filepath.Join(scripts, "main.scpt")) {
		t.Errorf("the old launcher is still in place")
	}
	set, _ := filepath.Glob(filepath.Join(in.Home, ".Trash", "Claude Client-d.app-*"))
	if len(set) != 1 {
		t.Errorf("set aside: %v", set)
	}
}

func TestInstallLeavesOpenCopy(t *testing.T) {
	in, sys := setup(t)
	makeCopy(t, in, "2.31226.0", true)
	sys.procs = []Process{{PID: 7, Command: in.Copy() + "/Contents/MacOS/Claude --user-data-dir=" + in.DataDir()}}
	if _, err := Install(in, sys, "/x/ai-profile"); err != nil {
		t.Fatal(err)
	}
	if sys.clones != 0 || sys.Version(in.Copy()) != "2.31226.0" {
		t.Errorf("an open copy is not replaced: clones %d", sys.clones)
	}
}

func TestCheck(t *testing.T) {
	in, sys := setup(t)
	s := Check(in, sys)
	if s.Launcher != LauncherMissing || s.Copy != "" || s.Source != "3.6556.2" {
		t.Errorf("state %+v", s)
	}
	Install(in, sys, "/x/ai-profile")
	os.RemoveAll(in.Copy())
	makeCopy(t, in, "2.31226.0", false)
	os.MkdirAll(in.Copy()+".refreshing", 0o755)
	sys.procs = []Process{{PID: 9, Command: in.Copy() + "/Contents/MacOS/Claude"}}
	s = Check(in, sys)
	if s.Launcher != LauncherOK || s.Copy != "2.31226.0" || s.Tinted || !slices.Equal(s.Leftovers, []string{in.Copy() + ".refreshing"}) || !slices.Equal(s.WithoutProfile, []int{9}) || !s.Running {
		t.Errorf("state %+v", s)
	}
	if !s.Behind() {
		t.Errorf("2.31226.0 is behind 3.6556.2")
	}
}

func TestCompare(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
	}{{"3.6556.2", "3.6556.2", 0}, {"2.31226.0", "3.6556.2", -1}, {"3.10.0", "3.9.9", 1}, {"3.6556", "3.6556.0", 0}} {
		if got := compare(c.a, c.b); got != c.want {
			t.Errorf("compare(%s, %s) = %d", c.a, c.b, got)
		}
	}
}
