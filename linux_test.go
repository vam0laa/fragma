//go:build linux

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuleset(t *testing.T) {
	r := ruleset(18443, 18053, true)
	for _, want := range []string{"meta mark 0x66726167 return", "ip daddr 127.0.0.0/8 return", "udp dport 53 redirect to :18053", "tcp dport 443 redirect to :18443"} {
		if !strings.Contains(r, want) {
			t.Errorf("ruleset misses %q", want)
		}
	}
	// the DNS rule must come before the private-range exclusion (LAN resolvers need it)
	if strings.Index(r, "udp dport 53") > strings.Index(r, "192.168.0.0/16") {
		t.Error("DNS redirect must come before the private range exclusion")
	}
	if strings.Contains(ruleset(18443, 18053, false), "udp dport 53") {
		t.Error("no DNS rule expected with dns=false")
	}
}

func TestInstallUninstall(t *testing.T) {
	dir := t.TempDir()
	// install copies the running binary; use any file as the source via the test binary itself
	runInstall([]string{"--prefix", dir, "--no-systemctl"})
	bin := filepath.Join(dir, binDest)
	if st, err := os.Stat(bin); err != nil || st.Mode()&0o111 == 0 {
		t.Fatalf("binary not installed/executable: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dir, unitDest))
	if err != nil || !strings.Contains(string(b), "ExecStart=/usr/local/bin/fragma run") ||
		!strings.Contains(string(b), "ExecStopPost=-/usr/bin/env nft delete table ip fragma") ||
		!strings.Contains(string(b), "StateDirectory=fragma") {
		t.Fatalf("unit file wrong: %v\n%s", err, b)
	}
	runUninstall([]string{"--prefix", dir, "--no-systemctl"})
	if _, err := os.Stat(bin); err == nil {
		t.Error("binary still there after uninstall")
	}
	if _, err := os.Stat(filepath.Join(dir, unitDest)); err == nil {
		t.Error("unit still there after uninstall")
	}
}

func TestInstallCreatesHostsFile(t *testing.T) {
	dir := t.TempDir()
	runInstall([]string{"--prefix", dir, "--no-systemctl"})
	b, err := os.ReadFile(filepath.Join(dir, defaultHostsFile))
	if err != nil || !strings.Contains(string(b), "Every other site is left completely alone") {
		t.Fatalf("hosts file missing or wrong: %v", err)
	}
	if got := parseHosts(strings.NewReader(string(b))); len(got) != 0 {
		t.Fatalf("the template must not contain active hosts, got %v", got)
	}
}

func TestAddHosts(t *testing.T) {
	dir := t.TempDir()
	runAdd([]string{"--prefix", dir, "Example.COM", "foo.org"})
	runAdd([]string{"--prefix", dir, "example.com"}) // duplicate: not added twice
	b, _ := os.ReadFile(filepath.Join(dir, defaultHostsFile))
	got := parseHosts(strings.NewReader(string(b)))
	if strings.Join(got, ",") != "example.com,foo.org" {
		t.Fatalf("hosts file = %v", got)
	}
}

func TestDNSDropIn(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, dropIn)
	runDNS([]string{"--prefix", dir, "--no-systemctl", "on", "--doh", "https://dns.example/dns-query"})
	b, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(b), "ExecStart=\nExecStart=/usr/local/bin/fragma run --dns --doh https://dns.example/dns-query") {
		t.Fatalf("drop-in wrong: %v\n%s", err, b)
	}
	runDNS([]string{"--prefix", dir, "--no-systemctl", "off"})
	if exists(path) {
		t.Fatal("drop-in must be removed by `dns off`")
	}
}
