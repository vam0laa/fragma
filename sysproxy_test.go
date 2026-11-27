package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeBackend struct {
	c       proxyConfig
	failSet bool
}

func (f *fakeBackend) get() (proxyConfig, error) { return f.c, nil }
func (f *fakeBackend) set(c proxyConfig) error {
	if f.failSet {
		return errors.New("set failed")
	}
	f.c = c
	return nil
}

func TestEnableAndRestoreProxy(t *testing.T) {
	orig := proxyConfig{Flags: flagDirect | 0x8, Bypass: "intranet.test"}
	b := &fakeBackend{c: orig}
	path := filepath.Join(t.TempDir(), "sub", "proxy.json")
	if err := enableProxy(b, path, "127.0.0.1:8899", 1); err != nil {
		t.Fatal(err)
	}
	if b.c.Server != "https=127.0.0.1:8899" {
		t.Fatalf("only HTTPS may go to the proxy, got %q", b.c.Server)
	}
	if b.c.Flags != orig.Flags|flagProxy || !strings.HasPrefix(b.c.Bypass, "intranet.test;") || !strings.Contains(b.c.Bypass, "172.31.*") {
		t.Fatalf("config = %+v", b.c)
	}
	// a second run after a crash must not save our own settings as the old ones
	if err := enableProxy(b, path, "127.0.0.1:8899", 2); err != nil {
		t.Fatal(err)
	}
	if err := restoreProxy(b, path); err != nil {
		t.Fatal(err)
	}
	if b.c != orig {
		t.Fatalf("restored %+v, want %+v", b.c, orig)
	}
	if exists := fileExists(path); exists {
		t.Fatal("state file must be removed")
	}
	if err := restoreProxy(b, path); err != nil || b.c != orig {
		t.Fatal("restoring twice must do nothing", err)
	}
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

func TestForeignProxyIsNotOverwritten(t *testing.T) {
	for _, c := range []proxyConfig{
		{Flags: flagDirect | flagProxy, Server: "corp:3128"},
		{Flags: flagDirect | flagPAC, PAC: "http://corp/proxy.pac"},
	} {
		b := &fakeBackend{c: c}
		path := filepath.Join(t.TempDir(), "proxy.json")
		if err := enableProxy(b, path, "127.0.0.1:8899", 1); !errors.Is(err, errForeignProxy) {
			t.Fatalf("err = %v", err)
		}
		if b.c != c || fileExists(path) {
			t.Fatal("nothing may change")
		}
	}
}

func TestRestoreKeepsLaterUserChange(t *testing.T) {
	b := &fakeBackend{}
	path := filepath.Join(t.TempDir(), "proxy.json")
	if err := enableProxy(b, path, "127.0.0.1:8899", 1); err != nil {
		t.Fatal(err)
	}
	mine := proxyConfig{Flags: flagProxy, Server: "new:1"}
	b.c = mine
	if err := restoreProxy(b, path); err != nil || b.c != mine || fileExists(path) {
		t.Fatal("a proxy set by someone else must stay", err, b.c)
	}
}

func TestFailedRestoreKeepsState(t *testing.T) {
	b := &fakeBackend{}
	path := filepath.Join(t.TempDir(), "proxy.json")
	if err := enableProxy(b, path, "127.0.0.1:8899", 1); err != nil {
		t.Fatal(err)
	}
	b.failSet = true
	if restoreProxy(b, path) == nil || !fileExists(path) {
		t.Fatal("the old settings must be kept for another try")
	}
}
