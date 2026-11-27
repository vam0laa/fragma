//go:build linux

package main

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Installing over an older version must never leave the system worse off: what
// was there is remembered, and put back if the new version does not come up.

const (
	keptSuffix     = ".old"      // the version before the last update
	rollbackSuffix = ".rollback" // exists only while an update is under way
)

// How long a new service gets to set its rules, and how long it must then stay.
const (
	readyTimeout = 40 * time.Second
	stableFor    = 3 * time.Second
)

// installation is what an install changes, as it was before.
type installation struct {
	bin, unit       string
	hadBin, hadUnit bool
	unitData        []byte
	active, enabled bool
}

func quietSystemctl(args ...string) error {
	c := exec.Command("systemctl", args...)
	c.Stderr = os.Stderr
	return c.Run()
}

// forgetFailures clears the count of failed starts; it fails when there is none.
func forgetFailures() { exec.Command("systemctl", "reset-failed", "fragma").Run() }

func enabled() bool { return exec.Command("systemctl", "is-enabled", "--quiet", "fragma").Run() == nil }

func mainPID() string {
	out, err := exec.Command("systemctl", "show", "-p", "MainPID", "--value", "fragma").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// snapshot remembers the present installation. The program is copied aside.
func snapshot(bin, unit string, service bool) (installation, error) {
	was := installation{bin: bin, unit: unit}
	b, err := os.ReadFile(unit)
	switch {
	case err == nil:
		was.hadUnit, was.unitData = true, b
	case !errors.Is(err, fs.ErrNotExist):
		return was, err
	}
	if exists(bin) {
		was.hadBin = true
		if err := copyFile(bin, bin+rollbackSuffix, 0o755); err != nil {
			return was, err
		}
	}
	if service {
		was.active, was.enabled = active(), enabled()
	}
	return was, nil
}

// comeUp waits until the service is ready, then makes sure it stays. ready tells
// whether it has done its start-up work.
func comeUp(ready func() bool) error {
	deadline := time.Now().Add(readyTimeout)
	for {
		time.Sleep(500 * time.Millisecond)
		if !active() {
			return errors.New(T("fail.stayup"))
		}
		if ready() {
			break
		}
		if time.Now().After(deadline) {
			return errors.New(T("fail.notready"))
		}
	}
	pid := mainPID()
	for end := time.Now().Add(stableFor); time.Now().Before(end); {
		time.Sleep(500 * time.Millisecond)
		// another process id means it ended and was started again
		if !active() || mainPID() != pid {
			return errors.New(T("fail.stayup"))
		}
	}
	return nil
}

// activate starts the freshly installed version.
func activate(ready func() bool) error {
	if err := systemctl("daemon-reload"); err != nil {
		return errors.New(T("fail.reload"))
	}
	if err := quietSystemctl("enable", "fragma"); err != nil {
		return errors.New(T("fail.enable"))
	}
	stepOK(T("step.service"), T("d.enabled"))
	forgetFailures()
	// restart: an upgrade must run the new binary
	if err := quietSystemctl("restart", "fragma"); err != nil {
		return errors.New(T("fail.start"))
	}
	return comeUp(ready)
}

// rollBack puts the earlier installation back and returns what to tell the user
// after "the new version did not work: <cause>".
func rollBack(was installation, service bool, ready func() bool) string {
	var problems []error
	note := func(err error) {
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			problems = append(problems, err)
		}
	}
	if service && !was.hadBin {
		quietSystemctl("disable", "--now", "fragma") // there was nothing before
	} else if service {
		quietSystemctl("stop", "fragma")
	}
	if was.hadBin {
		note(os.Rename(was.bin+rollbackSuffix, was.bin))
	} else {
		note(os.Remove(was.bin))
	}
	if was.hadUnit {
		note(writeFileAtomic(was.unit, was.unitData, 0o644))
	} else {
		note(os.Remove(was.unit))
	}
	if service {
		note(systemctl("daemon-reload"))
		if was.hadBin {
			if !was.enabled {
				note(quietSystemctl("disable", "fragma"))
			}
			if was.active {
				forgetFailures() // or systemd may refuse to start it again
				if err := quietSystemctl("start", "fragma"); err != nil {
					note(err)
				} else {
					note(comeUp(ready))
				}
			}
		}
	}
	switch {
	case len(problems) > 0:
		return T("upd.broken", errors.Join(problems...))
	case !was.hadBin:
		return T("upd.removed")
	case service && !was.active:
		return T("upd.restoredoff")
	}
	return T("upd.restored")
}

// keepPrevious turns the copy made for the update into the kept old version,
// unless it is the same program as the new one.
func keepPrevious(was installation) error {
	if !was.hadBin {
		return nil
	}
	old, err := os.ReadFile(was.bin + rollbackSuffix)
	if err != nil {
		return err
	}
	now, err := os.ReadFile(was.bin)
	if err != nil {
		return err
	}
	if bytes.Equal(old, now) {
		return os.Remove(was.bin + rollbackSuffix)
	}
	if err := os.Rename(was.bin+rollbackSuffix, was.bin+keptSuffix); err != nil {
		return fmt.Errorf("%s: %w", was.bin+keptSuffix, err)
	}
	stepOK(T("step.kept"), binDest+keptSuffix)
	return nil
}
