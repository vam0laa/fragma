//go:build windows

package main

// Changes the real proxy settings of the machine, so it only runs when asked for,
// on a throw-away machine such as a CI runner:
//
//	go build -o fragma.exe . && FRAGMA_WIN_INTEGRATION=1 FRAGMA_BIN=...\fragma.exe go test -run Integration -v .

import (
	"fmt"
	"net"
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

// Replacing a file the way the hosts file is replaced keeps its permissions.
func TestIntegrationReplaceKeepsPermissions(t *testing.T) {
	if os.Getenv("FRAGMA_WIN_INTEGRATION") == "" {
		t.Skip("set FRAGMA_WIN_INTEGRATION")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "hosts")
	if err := os.WriteFile(path, []byte("10.0.0.1 mine.test\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// a permission only this file has
	if out, err := exec.Command("icacls", path, "/grant", "*S-1-5-32-545:(R)").CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	acl := func() string {
		out, err := exec.Command("icacls", path).CombinedOutput()
		if err != nil {
			t.Fatal(err, string(out))
		}
		return string(out)
	}
	before := acl()
	for _, on := range []bool{true, false} {
		if _, err := updateHosts(path, on); err != nil {
			t.Fatal(err)
		}
		if after := acl(); after != before {
			t.Fatalf("permissions changed:\n%s\nto\n%s", before, after)
		}
	}
	if b, _ := os.ReadFile(path); string(b) != "10.0.0.1 mine.test\r\n" {
		t.Fatalf("content = %q", b)
	}
}

// The tests below prepare settings of their own, so they insist on GitHub's
// throw-away runners and cannot be switched on by accident on a person's machine.
func throwawayRunner(t *testing.T) {
	if os.Getenv("FRAGMA_WIN_INTEGRATION") == "" || os.Getenv("GITHUB_ACTIONS") != "true" {
		t.Skip("only on a GitHub Actions runner")
	}
}

// regLine is a value of the Internet Settings key as reg prints it, or "absent".
func regLine(name string) string {
	out, err := exec.Command("reg", "query", inetKey, "/v", name).Output()
	if err != nil {
		return "absent"
	}
	for _, l := range strings.Split(string(out), "\n") {
		if f := strings.Fields(l); len(f) >= 2 && f[0] == name {
			return strings.Join(f[1:], " ")
		}
	}
	return "absent"
}

const marker = "FragmaTestMarker"

// proxyValues is what must come back: the settings as the system reports them and
// the registry values programs read, with one value fragma has no business with.
func proxyValues(t *testing.T) map[string]string {
	c, err := (winProxy{}).get()
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]string{"flags": fmt.Sprint(c.Flags), "server": c.Server, "bypass": c.Bypass, "pac": c.PAC}
	for _, n := range []string{"ProxyEnable", "ProxyServer", "ProxyOverride", "AutoConfigURL", marker} {
		m["reg "+n] = regLine(n)
	}
	return m
}

func sameValues(t *testing.T, when string, want, got map[string]string) {
	t.Helper()
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: %s is %q, want %q", when, k, got[k], v)
		}
	}
}

// preset writes proxy settings a person might have before fragma comes.
func preset(t *testing.T, c proxyConfig) map[string]string {
	if err := (winProxy{}).set(c); err != nil {
		t.Fatal(err)
	}
	if err := regSet(inetKey, marker, "untouched"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reg("delete", inetKey, "/v", marker, "/f") })
	got := proxyValues(t)
	if got["server"] != c.Server || got["bypass"] != c.Bypass || got["reg ProxyServer"] != "REG_SZ "+c.Server {
		t.Fatalf("the settings to start from were not taken over: %v", got)
	}
	t.Logf("settings before fragma: %v", got)
	return got
}

// A proxy address, port and exception list that are there but switched off come
// back exactly, whether fragma is stopped normally or killed.
func TestIntegrationPresetProxyComesBack(t *testing.T) {
	throwawayRunner(t)
	for _, end := range []string{"stop", "kill, then the guard", "kill both, then restore"} {
		t.Run(end, func(t *testing.T) {
			bin, _, state := integration(t)
			before := preset(t, proxyConfig{Flags: flagDirect, Server: "proxy.example.test:3128", Bypass: "*.example.test;10.1.2.3;<local>"})
			cmd := startRun(t, bin)
			during := proxyValues(t)
			t.Logf("settings while fragma runs: %v", during)
			if during["server"] != testAddr || !strings.HasPrefix(during["bypass"], "*.example.test;10.1.2.3;<local>;") || during["reg "+marker] != before["reg "+marker] {
				t.Fatalf("fragma must set its proxy and keep the exceptions: %v", during)
			}
			switch end {
			case "stop":
				if out, err := exec.Command(bin, "stop").CombinedOutput(); err != nil {
					t.Fatal(err, string(out))
				}
			case "kill, then the guard":
				if err := cmd.Process.Kill(); err != nil {
					t.Fatal(err)
				}
				waitFor(t, "the guard restores", func(c proxyConfig) bool { return c.Server != testAddr })
				time.Sleep(time.Second)
			default:
				killAll(t, " guard ")
				cmd.Process.Kill()
				time.Sleep(2 * time.Second)
				if c, _ := (winProxy{}).get(); c.Server != testAddr {
					t.Fatalf("nothing should have restored yet: %+v", c)
				}
				if out, err := exec.Command(bin, "restore").CombinedOutput(); err != nil {
					t.Fatal(err, string(out))
				}
			}
			after := proxyValues(t)
			t.Logf("settings after fragma: %v", after)
			sameValues(t, "after fragma", before, after)
			if _, err := os.Stat(state); !os.IsNotExist(err) {
				t.Error("state file left behind:", err)
			}
		})
	}
}

// What the person changes while fragma runs stays; only fragma's part goes.
func TestIntegrationLaterChangeSurvives(t *testing.T) {
	throwawayRunner(t)
	bin, _, _ := integration(t)
	before := preset(t, proxyConfig{Flags: flagDirect, Server: "proxy.example.test:3128", Bypass: "*.example.test"})
	startRun(t, bin)
	c, err := (winProxy{}).get()
	if err != nil {
		t.Fatal(err)
	}
	c.Bypass, c.Reg = "added.example.test;"+c.Bypass, registryValues{}
	if err := (winProxy{}).set(c); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(bin, "stop").CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	want := map[string]string{}
	for k, v := range before {
		want[k] = v
	}
	want["bypass"] = "added.example.test;*.example.test"
	want["reg ProxyOverride"] = "REG_SZ added.example.test;*.example.test"
	after := proxyValues(t)
	t.Logf("settings after fragma: %v", after)
	sameValues(t, "after fragma", want, after)
}

// A proxy or setup script that is in use is not touched at all.
func TestIntegrationProxyInUseIsLeftAlone(t *testing.T) {
	throwawayRunner(t)
	for name, c := range map[string]proxyConfig{
		"proxy":        {Flags: flagDirect | flagProxy, Server: "corp.example.test:8080", Bypass: "intranet.example.test"},
		"setup script": {Flags: flagDirect | flagPAC, Server: "old.example.test:1", Bypass: "a.example.test", PAC: "http://pac.example.test/proxy.pac"},
	} {
		t.Run(name, func(t *testing.T) {
			bin, _, state := integration(t)
			before := preset(t, c)
			cmd := exec.Command(bin, "run", "--port", "18899")
			done := make(chan error, 1)
			var out strings.Builder
			cmd.Stdout, cmd.Stderr = &out, &out
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			go func() { done <- cmd.Wait() }()
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("fragma must refuse to start:", out.String())
				}
			case <-time.After(15 * time.Second):
				t.Fatal("fragma started over a proxy in use")
			}
			t.Log(strings.TrimSpace(out.String()))
			sameValues(t, "after the refusal", before, proxyValues(t))
			if _, err := os.Stat(state); !os.IsNotExist(err) {
				t.Error("state file written:", err)
			}
			if reg("query", runOnceKey, "/v", "fragma-restore") == nil {
				t.Error("a sign-in entry was left behind")
			}
		})
	}
}

// permissions lists owner, access rules and attributes of a file.
func permissions(t *testing.T, path string) string {
	script := `$p='` + path + `'; (Get-Acl -LiteralPath $p).Sddl; (Get-Item -LiteralPath $p -Force).Attributes`
	out, err := exec.Command("powershell", "-NoProfile", "-Command", script).CombinedOutput()
	if err != nil {
		t.Fatal(err, string(out))
	}
	return strings.TrimSpace(string(out))
}

// `fragma apps on` and `off` on the real hosts file give it back byte for byte
// with the same permissions.
func TestIntegrationRealHostsFile(t *testing.T) {
	throwawayRunner(t)
	bin, _, _ := integration(t)
	t.Setenv("FRAGMA_LANG", "en")
	path := systemHosts()
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	originalPerms := permissions(t, path)
	backup := path + hostsBackup
	if _, err := os.Stat(backup); !os.IsNotExist(err) {
		t.Fatal("a backup is already there:", err)
	}
	// registered before anything is changed, so it runs however the test ends
	t.Cleanup(func() {
		if err := os.WriteFile(path, original, 0o644); err != nil { // keeps the permissions
			t.Error("CLEANUP could not restore the hosts file:", err)
		}
		os.Remove(backup)
		exec.Command("ipconfig", "/flushdns").Run()
		if now, err := os.ReadFile(path); err != nil || string(now) != string(original) {
			t.Error("CLEANUP: the hosts file differs from the original", err)
		}
	})
	apps := func(arg string) {
		t.Helper()
		out, err := exec.Command(bin, "apps", arg).CombinedOutput()
		t.Logf("fragma apps %s: %s", arg, strings.TrimSpace(string(out)))
		if err != nil {
			t.Fatal(err)
		}
	}
	content := func() string {
		t.Helper()
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	// the runner's own file, then one with a line of the person's and no final newline
	for name, start := range map[string]string{
		"as it is":             string(original),
		"own line, no newline": string(original) + "\r\n10.9.8.7 mine.example.test",
	} {
		if err := os.WriteFile(path, []byte(start), 0o644); err != nil {
			t.Fatal("administrator rights are needed:", err)
		}
		apps("on")
		on := content()
		if !strings.HasPrefix(on, start) || !strings.Contains(on, hostsBegin) || !strings.Contains(on, "127.0.0.1 updates.discord.com") {
			t.Fatalf("%s: after on:\n%s", name, on)
		}
		if b, err := os.ReadFile(backup); err != nil || string(b) != start {
			t.Fatalf("%s: the backup is not the content from before: %v %q", name, err, b)
		}
		if p := permissions(t, path); p != originalPerms {
			t.Fatalf("%s: permissions after on:\n%s\nbefore:\n%s", name, p, originalPerms)
		}
		// the system really uses the entry
		exec.Command("ipconfig", "/flushdns").Run()
		ips, err := net.LookupIP("updates.discord.com")
		if err != nil || len(ips) == 0 {
			t.Fatal("lookup:", err)
		}
		for _, ip := range ips {
			if !ip.IsLoopback() {
				t.Fatalf("%s: updates.discord.com still leads to %v", name, ips)
			}
		}
		apps("on") // a second time changes nothing
		if content() != on {
			t.Fatalf("%s: a second 'on' changed the file", name)
		}
		apps("off")
		if got := content(); got != start {
			t.Fatalf("%s: after off %q, want %q", name, got, start)
		}
		if p := permissions(t, path); p != originalPerms {
			t.Fatalf("%s: permissions after off:\n%s\nbefore:\n%s", name, p, originalPerms)
		}
		t.Logf("%s: %d bytes back exactly, permissions unchanged: %s", name, len(start), originalPerms)
	}
}
