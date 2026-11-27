package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// On and off again must give back the file byte for byte, however it ends.
func TestHostsRoundTripIsExact(t *testing.T) {
	for _, orig := range []string{
		"", "127.0.0.1 localhost", "127.0.0.1 localhost\n", "127.0.0.1 localhost\r\n", "# mine\r\n\r\n127.0.0.1 localhost",
		"\ufeff127.0.0.1 localhost\r\n\r\n",
	} {
		on := editHosts(orig, true)
		if !strings.Contains(on, hostsBegin) || editHosts(on, true) != on {
			t.Fatalf("%q: on = %q", orig, on)
		}
		if got := editHosts(on, false); got != orig {
			t.Errorf("%q: after on and off %q", orig, got)
		}
	}
}

func TestHostsLinesAddedAfterOurBlockStay(t *testing.T) {
	on := editHosts("a", true) + "\n10.0.0.1 later.test\n"
	if got := editHosts(on, false); got != "a\n10.0.0.1 later.test\n" {
		t.Fatalf("off = %q", got)
	}
}

func hostsIn(t *testing.T, content string) string {
	p := filepath.Join(t.TempDir(), "hosts")
	if err := os.WriteFile(p, []byte(content), 0o640); err != nil {
		t.Fatal(err)
	}
	return p
}

func read(t *testing.T, p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func leftovers(t *testing.T, p string) []string {
	m, _ := filepath.Glob(filepath.Join(filepath.Dir(p), "*.tmp"))
	return m
}

func TestUpdateHostsKeepsABackup(t *testing.T) {
	const orig = "10.0.0.1 mine.test\r\n"
	p := hostsIn(t, orig)
	changed, err := updateHosts(p, true)
	if err != nil || !changed {
		t.Fatal(changed, err)
	}
	if read(t, p+hostsBackup) != orig || !strings.Contains(read(t, p), hostsBegin) {
		t.Fatal("the content from before the change must be kept")
	}
	if st, _ := os.Stat(p); st.Mode().Perm() != 0o640 && os.PathSeparator == '/' {
		t.Error("the file mode changed to", st.Mode().Perm())
	}
	// nothing to change: nothing written, the backup is not replaced
	if changed, err := updateHosts(p, true); err != nil || changed || read(t, p+hostsBackup) != orig {
		t.Fatal("a second 'on' must change nothing", changed, err)
	}
	if _, err := updateHosts(p, false); err != nil || read(t, p) != orig {
		t.Fatal(err, read(t, p))
	}
	if l := leftovers(t, p); len(l) != 0 {
		t.Error("temporary files left:", l)
	}
}

// If the new file cannot be put in place, the old one stays whole.
func TestUpdateHostsFailureLeavesTheFileAlone(t *testing.T) {
	const orig = "10.0.0.1 mine.test\n"
	p := hostsIn(t, orig)
	old := replaceFile
	replaceFile = func(string, string) error { return errors.New("file is in use") }
	defer func() { replaceFile = old }()
	if _, err := updateHosts(p, true); err == nil {
		t.Fatal("the failure must be reported")
	}
	if read(t, p) != orig || read(t, p+hostsBackup) != orig || len(leftovers(t, p)) != 0 {
		t.Fatal("file changed or temporary file left", read(t, p), leftovers(t, p))
	}
}

// Without a backup nothing is changed.
func TestUpdateHostsNeedsItsBackup(t *testing.T) {
	const orig = "x\n"
	p := hostsIn(t, orig)
	if err := os.Mkdir(p+hostsBackup, 0o755); err != nil { // a folder where the backup would go
		t.Fatal(err)
	}
	if _, err := updateHosts(p, true); err == nil || read(t, p) != orig {
		t.Fatal("must fail and change nothing", err, read(t, p))
	}
}
