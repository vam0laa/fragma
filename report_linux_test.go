//go:build linux

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCollectLinux(t *testing.T) {
	dir := t.TempDir()
	put := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	journal, journalErr := "fragma is on (HTTPS only, DNS untouched).\nalice 203.0.113.77\n", ""
	s := linuxSources{
		osRelease: put("os-release", "PRETTY_NAME=\"alice's my-laptop\"\nID=\"alice laptop\"\nVERSION_ID=\"203.0.113.77\"\n"),
		kernel:    put("kernel", "6.9.1-alice-my-laptop\n"),
		binary:    put("fragma", ""),
		dropIn:    filepath.Join(dir, "none"),
		hosts:     put("hosts", "secret-site.example\nother.example\n"),
		learned:   filepath.Join(dir, "none"),
		run: func(name string, args ...string) (string, string, error) {
			switch {
			case len(args) == 1 && args[0] == "version":
				return "fragma 0.1.2\n", "", nil
			case strings.HasSuffix(name, "nft"):
				return "nftables v1.1.7 (alice)\n", "", nil
			case name == "systemctl" && args[0] == "is-active":
				return "", "", nil
			case name == "systemctl":
				return "", "", errors.New("disabled")
			}
			return journal, journalErr, nil
		},
	}
	d := collectLinux(s)
	if d.name != "unknown" || d.version != "unknown" || d.linux.Kernel != "6.9.1" || d.linux.Nftables != "1.1.7" {
		t.Errorf("%q %q %+v", d.name, d.version, d.linux)
	}
	if d.state.InstalledVersion != "0.1.2" || d.state.DNSOverHTTPS != nil {
		t.Errorf("an old installed version: %+v", d.state)
	}
	if !*d.state.Running || *d.linux.ServiceEnabled || *d.state.OwnSites != 2 || *d.state.LearnedSites != 0 {
		t.Errorf("state %+v", d.state)
	}
	r := buildReport(context.Background(), d, reportNetwork{OtherBypassTools: []string{}}, false, time.Now())
	assertNoSecrets(t, "JSON", r.json())
	assertNoSecrets(t, "text", r.text())
	if r.LogStatus != "read" || r.LogUnknown != 1 {
		t.Error(r.LogStatus, r.LogUnknown)
	}

	journal, journalErr = "-- No entries --\n", "Hint: not seeing messages? insufficient permissions"
	s.run = func(name string, args ...string) (string, string, error) {
		if name == "journalctl" {
			return journal, journalErr, nil
		}
		return "", "", errors.New("missing")
	}
	d = collectLinux(s)
	if d.logStatus != "not_readable" || d.linux.Nftables != "missing" {
		t.Error(d.logStatus, d.linux.Nftables)
	}
	journalErr = ""
	if d = collectLinux(s); d.logStatus != "none" {
		t.Error(d.logStatus)
	}
}
