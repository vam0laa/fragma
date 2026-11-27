package main

import (
	"os"
	"path/filepath"
	"testing"
)

// what a running fragma leaves in the system settings
func ourSettings() proxyConfig {
	c, _ := ourConfig(proxyConfig{Flags: flagDirect}, "127.0.0.1:8899")
	return c
}

// captureWarnings collects what the user would be told.
func captureWarnings(t *testing.T) *[]string {
	var got []string
	old := proxyWarn
	proxyWarn = func(key string, _ ...any) { got = append(got, key) }
	t.Cleanup(func() { proxyWarn = old })
	return &got
}

var damaged = map[string]string{"empty": "", "cut off": `{"pid":1,"ours":"127.0.0.1:8899","old":{"fla`, "no content": `{}`, "zeroes": "\x00\x00\x00\x00"}

// A state file broken by a power cut must not leave the proxy pointing at a dead port.
func TestDamagedStateTurnsOurProxyOff(t *testing.T) {
	for name, content := range damaged {
		t.Run(name, func(t *testing.T) {
			warned := captureWarnings(t)
			path := filepath.Join(t.TempDir(), "proxy.json")
			os.WriteFile(path, []byte(content), 0o600)
			b := &fakeBackend{c: ourSettings()}
			b.c.Bypass = "intranet.test;" + b.c.Bypass
			if err := restoreProxy(b, path); err != nil {
				t.Fatal(err)
			}
			if b.c.Server != "" || b.c.Flags&flagProxy != 0 || b.c.Bypass != "intranet.test" {
				t.Fatalf("settings = %+v", b.c)
			}
			if len(*warned) != 1 || (*warned)[0] != "win.statelost" {
				t.Fatalf("the user must be told, got %v", *warned)
			}
			if fileExists(path) || !fileExists(path+".damaged") {
				t.Fatal("the damaged file must be put aside")
			}
			if err := enableProxy(b, path, "127.0.0.1:8899", 2); err != nil {
				t.Fatal("fragma must be able to start again:", err)
			}
		})
	}
}

func TestDamagedStateLeavesOtherProxyAlone(t *testing.T) {
	warned := captureWarnings(t)
	path := filepath.Join(t.TempDir(), "proxy.json")
	os.WriteFile(path, nil, 0o600)
	mine := proxyConfig{Flags: flagProxy, Server: "127.0.0.1:3128", Bypass: "a.test"}
	b := &fakeBackend{c: mine}
	if err := restoreProxy(b, path); err != nil || b.c != mine {
		t.Fatal("a proxy that is not ours must stay", err, b.c)
	}
	if len(*warned) != 1 || (*warned)[0] != "win.statebad" {
		t.Fatalf("warnings = %v", *warned)
	}
}

func TestDamagedStateFallsBackToTheSecondCopy(t *testing.T) {
	warned := captureWarnings(t)
	orig := proxyConfig{Flags: flagDirect, Server: "old.test:8", Bypass: "intranet.test"}
	path := filepath.Join(t.TempDir(), "proxy.json")
	b := &fakeBackend{c: orig}
	if err := enableProxy(b, path, "127.0.0.1:8899", 1); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(path, nil, 0o600)
	if err := restoreProxy(b, path); err != nil || b.c != orig {
		t.Fatalf("restored %+v (%v), want %+v", b.c, err, orig)
	}
	if len(*warned) != 0 || fileExists(path) || fileExists(path+".bak") {
		t.Fatal("a good second copy is a normal restore", *warned)
	}
}

func TestDamagedStateStaysWhenSettingsCannotBeChanged(t *testing.T) {
	captureWarnings(t)
	path := filepath.Join(t.TempDir(), "proxy.json")
	os.WriteFile(path, nil, 0o600)
	b := &fakeBackend{c: ourSettings(), failSet: true}
	if restoreProxy(b, path) == nil || !fileExists(path) {
		t.Fatal("the failure must be reported and the file kept for another try")
	}
}

// What the user changed while fragma ran stays; the rest goes back.
func TestRestoreKeepsOtherLaterChanges(t *testing.T) {
	orig := proxyConfig{Flags: flagDirect, Bypass: "intranet.test", Reg: registryValues{Known: true, Server: regValue{Set: true, Value: "kept.test:1"}}}
	path := filepath.Join(t.TempDir(), "proxy.json")
	b := &fakeBackend{c: orig}
	if err := enableProxy(b, path, "127.0.0.1:8899", 1); err != nil {
		t.Fatal(err)
	}
	b.c.Bypass = "added.test;" + b.c.Bypass
	b.c.PAC = "http://mine.test/proxy.pac"
	b.c.Flags |= flagPAC
	if err := restoreProxy(b, path); err != nil {
		t.Fatal(err)
	}
	if b.c.Server != "" || b.c.Flags != flagDirect|flagPAC || b.c.PAC != "http://mine.test/proxy.pac" || b.c.Bypass != "added.test;intranet.test" {
		t.Fatalf("settings = %+v", b.c)
	}
	if b.c.Reg.Server != orig.Reg.Server {
		t.Fatalf("the registry value from before must come back, got %+v", b.c.Reg)
	}
}

// A run that was killed leaves its file behind; the next one restores the true original.
func TestKilledRunIsRecovered(t *testing.T) {
	orig := proxyConfig{Flags: flagDirect | 0x8, Server: "off.test:1", Bypass: "intranet.test"}
	path := filepath.Join(t.TempDir(), "proxy.json")
	b := &fakeBackend{c: orig}
	for pid := 1; pid <= 3; pid++ { // three runs, none cleans up
		if err := enableProxy(b, path, "127.0.0.1:8899", pid); err != nil {
			t.Fatal(err)
		}
	}
	if err := restoreProxy(b, path); err != nil || b.c != orig {
		t.Fatalf("restored %+v (%v), want %+v", b.c, err, orig)
	}
}

// State files written by 0.4.0 still work.
func TestOldStateFileIsRestored(t *testing.T) {
	path := filepath.Join(t.TempDir(), "proxy.json")
	os.WriteFile(path, []byte(`{"pid":1,"ours":"127.0.0.1:8899","old":{"flags":1,"server":"","bypass":"x.test","pac":""}}`), 0o600)
	b := &fakeBackend{c: ourSettings()}
	if err := restoreProxy(b, path); err != nil || b.c != (proxyConfig{Flags: flagDirect, Bypass: "x.test"}) {
		t.Fatalf("restored %+v (%v)", b.c, err)
	}
}
