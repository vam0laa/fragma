//go:build windows

package main

// Changes the real proxy settings of the machine, so it only runs when asked for,
// on a throw-away machine such as a CI runner:
//
//	go build -o fragma.exe . && FRAGMA_WIN_INTEGRATION=1 FRAGMA_BIN=...\fragma.exe go test -run Integration -v .

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testAddr = "127.0.0.1:18899"

func integration(t *testing.T) (bin string, original proxyConfig, state string) {
	bin = os.Getenv("FRAGMA_BIN")
	if os.Getenv("FRAGMA_WIN_INTEGRATION") == "" || bin == "" {
		t.Skip("changes the system settings: set FRAGMA_WIN_INTEGRATION and FRAGMA_BIN")
	}
	original, err := (winProxy{}).get()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	t.Setenv("LOCALAPPDATA", filepath.Join(dir, "local"))
	t.Setenv("APPDATA", filepath.Join(dir, "roaming"))
	t.Setenv("FRAGMA_NO_DIALOG", "1")
	t.Cleanup(func() {
		killAll(t, "")
		(winProxy{}).set(original)
		reg("delete", runOnceKey, "/v", "fragma-restore", "/f")
	})
	return bin, original, filepath.Join(dir, "local", "fragma", "proxy.json")
}

// killAll ends fragma processes the hard way; with a word, only those whose command line has it.
func killAll(t *testing.T, word string) {
	script := `Get-CimInstance Win32_Process -Filter "Name='fragma.exe'" | Where-Object { $_.CommandLine -like '*` + word + `*' } | ForEach-Object { Stop-Process -Id $_.ProcessId -Force }`
	if out, err := exec.Command("powershell", "-NoProfile", "-Command", script).CombinedOutput(); err != nil {
		t.Log("stopping processes:", err, string(out))
	}
}

func waitFor(t *testing.T, what string, ok func(proxyConfig) bool) proxyConfig {
	var c proxyConfig
	var err error
	for end := time.Now().Add(20 * time.Second); time.Now().Before(end); time.Sleep(200 * time.Millisecond) {
		if c, err = (winProxy{}).get(); err == nil && ok(c) {
			return c
		}
	}
	t.Fatalf("%s: settings are %+v (%v)", what, c, err)
	return c
}

func startRun(t *testing.T, bin string) *exec.Cmd {
	cmd := exec.Command(bin, "run", "--port", "18899")
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go cmd.Wait()
	waitFor(t, "fragma sets its proxy", func(c proxyConfig) bool { return c.Server == testAddr })
	// the guard starts right after the settings change
	time.Sleep(2 * time.Second)
	return cmd
}

// The program is killed; its guard puts the settings back.
func TestIntegrationGuardRestoresAfterKill(t *testing.T) {
	bin, original, state := integration(t)
	cmd := startRun(t, bin)
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the guard restores", func(c proxyConfig) bool { return c == original })
	if _, err := os.Stat(state); !os.IsNotExist(err) {
		t.Error("state file left behind:", err)
	}
}

// Program and guard both die, as in a power cut; `restore` (run at sign-in) repairs it.
func TestIntegrationRestoreAfterBothDie(t *testing.T) {
	bin, original, _ := integration(t)
	cmd := startRun(t, bin)
	killAll(t, " guard ")
	cmd.Process.Kill()
	time.Sleep(2 * time.Second)
	if c, _ := (winProxy{}).get(); c.Server != testAddr {
		t.Fatalf("nothing should have restored yet: %+v", c)
	}
	out, err := exec.Command("reg", "query", runOnceKey, "/v", "fragma-restore").CombinedOutput()
	if err != nil || !strings.Contains(string(out), "restore") {
		t.Fatal("no sign-in recovery entry:", err, string(out))
	}
	if out, err := exec.Command(bin, "restore").CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	if c, _ := (winProxy{}).get(); c != original {
		t.Fatalf("restored %+v, want %+v", c, original)
	}
}

// The same with the saved settings destroyed: the proxy must still be turned off.
func TestIntegrationDamagedState(t *testing.T) {
	bin, _, state := integration(t)
	cmd := startRun(t, bin)
	killAll(t, " guard ")
	cmd.Process.Kill()
	time.Sleep(2 * time.Second)
	for _, p := range []string{state, state + ".bak"} {
		if err := os.WriteFile(p, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if out, err := exec.Command(bin, "restore").CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	c, _ := (winProxy{}).get()
	if c.Flags&flagProxy != 0 || c.Server != "" {
		t.Fatalf("the proxy must be off: %+v", c)
	}
}
