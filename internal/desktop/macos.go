package desktop

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

//go:embed tint.js
var tintJS []byte

const lsregister = "/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister"

// MacOS is System on the real Mac.
type MacOS struct{}

func (MacOS) Version(app string) string {
	out, err := exec.Command("/usr/bin/plutil", "-extract", "CFBundleShortVersionString", "raw", "-o", "-", filepath.Join(app, "Contents", "Info.plist")).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// Clone copies with APFS clones (cp -c), which take no space until a file
// changes, and falls back to a plain copy on other file systems.
func (MacOS) Clone(src, dst string) error {
	if out, err := exec.Command("/bin/cp", "-Rc", src, dst).CombinedOutput(); err != nil {
		os.RemoveAll(dst)
		if out2, err2 := exec.Command("/bin/cp", "-R", src, dst).CombinedOutput(); err2 != nil {
			return fmt.Errorf("%v: %s / %s", err2, strings.TrimSpace(string(out)), strings.TrimSpace(string(out2)))
		}
	}
	return nil
}

func (MacOS) Tint(app, iconFrom string, hue float64) error {
	f, err := os.CreateTemp("", "ai-profile-tint-*.js")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(tintJS); err != nil {
		f.Close()
		return err
	}
	f.Close()
	out, err := exec.Command("/usr/bin/osascript", "-l", "JavaScript", f.Name(), app, iconFrom, strconv.FormatFloat(hue, 'f', -1, 64)).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

var psLine = regexp.MustCompile(`^\s*(\d+)\s+(.*)$`)

func (MacOS) Processes() ([]Process, error) {
	out, err := exec.Command("/bin/ps", "-axww", "-o", "pid=,command=").Output()
	if err != nil {
		return nil, err
	}
	var procs []Process
	for _, line := range strings.Split(string(out), "\n") {
		if m := psLine.FindStringSubmatch(line); m != nil {
			pid, _ := strconv.Atoi(m[1])
			procs = append(procs, Process{PID: pid, Command: m[2]})
		}
	}
	return procs, nil
}

// Quit sends SIGTERM, which Electron handles like Quit from the menu, and
// waits up to 15 seconds.
func (MacOS) Quit(pids []int) bool {
	signal := func(pid int, sig os.Signal) error {
		p, err := os.FindProcess(pid)
		if err != nil {
			return err
		}
		return p.Signal(sig)
	}
	for _, pid := range pids {
		signal(pid, syscall.SIGTERM)
	}
	for i := 0; i < 60; i++ {
		alive := false
		for _, pid := range pids {
			alive = alive || signal(pid, syscall.Signal(0)) == nil
		}
		if !alive {
			return true
		}
		time.Sleep(250 * time.Millisecond)
	}
	return false
}

func (MacOS) Ask(title, msg, yes, no string) bool {
	script := []string{
		"on run argv",
		"activate",
		"try",
		"set r to display dialog (item 2 of argv) with title (item 1 of argv) buttons {item 4 of argv, item 3 of argv} default button (item 3 of argv) cancel button (item 4 of argv) with icon caution",
		"return button returned of r",
		"on error",
		"return \"\"",
		"end try",
		"end run",
	}
	args := []string{}
	for _, l := range script {
		args = append(args, "-e", l)
	}
	out, err := exec.Command("/usr/bin/osascript", append(args, title, msg, yes, no)...).Output()
	return err == nil && strings.TrimSpace(string(out)) == yes
}

func (MacOS) Open(args []string) error {
	if out, err := exec.Command("/usr/bin/open", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("open: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (MacOS) Register(app string) {
	exec.Command(lsregister, "-f", app).Run()
}

// Applet compiles source with osacompile, sets the keys in its Info.plist and
// signs it again ad hoc, since the Info.plist is part of the signature.
func (MacOS) Applet(app, source string, keys map[string]string) error {
	f, err := os.CreateTemp("", "ai-profile-launcher-*.applescript")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(source); err != nil {
		f.Close()
		return err
	}
	f.Close()
	run := func(name string, args ...string) error {
		if out, err := exec.Command(name, args...).CombinedOutput(); err != nil {
			return fmt.Errorf("%s: %v: %s", filepath.Base(name), err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	if err := run("/usr/bin/osacompile", "-o", app, f.Name()); err != nil {
		return err
	}
	plist := filepath.Join(app, "Contents", "Info.plist")
	for k, v := range keys {
		if err := run("/usr/bin/plutil", "-replace", k, "-string", v, plist); err != nil {
			return err
		}
	}
	return run("/usr/bin/codesign", "--force", "--sign", "-", app)
}
