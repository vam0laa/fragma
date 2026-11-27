package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"
)

// noNetwork replaces the site test.
func noNetwork(t *testing.T) {
	old := siteTester
	siteTester = func(_ context.Context, host string, _ bool, _ platformData) siteReport {
		return siteReport{Host: host, Name: "ok", Route: "direct", AsIs: &tryResult{Answered: 3, Tries: 3}}
	}
	t.Cleanup(func() { siteTester = old })
}

var secrets = []string{
	"alice", "secret-site.example", "203.0.113.77", "2001:db8::7", "/home/", `C:\Users`, "my-laptop", "hunter2",
}

func assertNoSecrets(t *testing.T, what string, data []byte) {
	t.Helper()
	for _, s := range secrets {
		if bytes.Contains(bytes.ToLower(data), []byte(strings.ToLower(s))) {
			t.Errorf("%s holds %q:\n%s", what, s, data)
		}
	}
	if a := regexp.MustCompile(`\b\d{1,3}(\.\d{1,3}){3}\b`).Find(data); a != nil {
		t.Errorf("%s holds an address: %s", what, a)
	}
}

var secretLog = []string{
	"fragma is on (HTTPS only, DNS untouched). alice 203.0.113.77",
	"cannot look up secret-site.example: lookup on 203.0.113.77:53 (DNS is left as it was)",
	`app relay: listen tcp 127.0.0.1:443: bind: C:\Users\alice`,
	"alice opened secret-site.example from 2001:db8::7",
	"password=hunter2 on my-laptop in /home/alice/.config",
	fmt.Sprintf(catalog["en"]["win.statelost"], `C:\Users\alice\AppData\Local\fragma\proxy.json.damaged`),
	fmt.Sprintf(catalog["tr"]["win.restorefail"], "/home/alice"),
}

func TestReportCarriesNoTextFromTheComputer(t *testing.T) {
	data := platformData{name: "arch", logStatus: "read", logLines: secretLog, proxyAddr: "127.0.0.1:8899"}
	r := buildReport(context.Background(), data, reportNetwork{OtherBypassTools: []string{}}, false, time.Now())
	for _, old := range []string{"en", "tr"} {
		prev := lang
		lang = old
		assertNoSecrets(t, "text", r.text())
		lang = prev
	}
	assertNoSecrets(t, "JSON", r.json())
	want := []logEvent{{"started", 1}, {"doh_lookup_failed", 1}, {"app_relay_failed", 1}, {"state_damaged", 1}, {"restore_failed", 1}}
	if !slices.Equal(r.LogEvents, want) || r.LogUnknown != 2 {
		t.Errorf("events %v, other lines %d", r.LogEvents, r.LogUnknown)
	}
}

func TestEveryTranslatedMessageIsRecognised(t *testing.T) {
	for key, code := range logMessages {
		for l, m := range catalog {
			if got := logCode("  ✖ " + fmt.Sprintf(m[key], "x")); m[key] == "" || got != code {
				t.Errorf("%s %s: %q, want %q", l, key, got, code)
			}
		}
	}
}

func keysOf(t *testing.T, b []byte) []string {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	var k []string
	for name := range m {
		k = append(k, name)
	}
	sort.Strings(k)
	return k
}

// The form is the same on every system; a change to it is a change of schema.
func TestReportSchema(t *testing.T) {
	linux := platformData{name: "arch", logStatus: "none", linux: &linuxInfo{}}
	windows := platformData{name: "windows", logStatus: "none", windows: &windowsInfo{}}
	want := []string{"checks", "date", "failed_checks", "fragma_version", "linux", "log_events", "log_status", "network",
		"schema", "sites", "sites_tested", "state", "system", "unrecognised_log_lines", "windows"}
	for _, d := range []platformData{linux, windows, {}} {
		b := buildReport(context.Background(), d, reportNetwork{OtherBypassTools: []string{}}, false, time.Now()).json()
		if got := keysOf(t, b); !slices.Equal(got, want) {
			t.Errorf("keys %v", got)
		}
		if bytes.Contains(b, []byte(`"sites": null`)) || bytes.Contains(b, []byte(`"log_events": null`)) {
			t.Error("lists must be empty, not null")
		}
	}
}

func TestSiteChecks(t *testing.T) {
	good, bad := &tryResult{Answered: 3, Tries: 3}, &tryResult{Tries: 3, Failure: "reset"}
	site := func(host, name string, asIs, frag *tryResult) siteReport {
		return siteReport{Host: host, Name: name, AsIs: asIs, Fragmented: frag}
	}
	codes := func(s ...siteReport) string {
		var out []string
		for _, c := range siteChecks(s) {
			out = append(out, c.Code)
		}
		return strings.Join(out, ",")
	}
	control := site(controlHost, "ok", good, nil)
	for want, got := range map[string]string{
		",,":                           codes(control, site("discord.com", "ok", good, nil)),
		",,FR_SITE_FAILS":              codes(control, site("discord.com", "ok", bad, bad)),
		",FR_DNS_PRIVATE,":             codes(control, site("discord.com", "private_address", good, nil)),
		",FR_DNS_FAILED,FR_SITE_FAILS": codes(control, site("discord.com", "failed", nil, nil)),
		"FR_NO_NETWORK,,":              codes(site(controlHost, "ok", bad, nil)),
	} {
		if want != got {
			t.Errorf("codes %q, want %q", got, want)
		}
	}
	// off: blocked as is but open when fragmented is what fragma is for
	if got := codes(control, site("discord.com", "ok", bad, good)); got != ",," {
		t.Errorf("got %q", got)
	}
}

func TestReportCommandShowsAndSavesTheSame(t *testing.T) {
	noNetwork(t)
	dir := t.TempDir()
	for _, name := range []string{"r.json", "r.txt"} {
		path := filepath.Join(dir, name)
		var out, info bytes.Buffer
		if err := report(context.Background(), []string{"--no-net", "--output", path}, &out, &info); err != nil {
			t.Fatal(err)
		}
		saved := []byte(read(t, path))
		if !bytes.Equal(saved, out.Bytes()) || !strings.Contains(info.String(), path) {
			t.Fatalf("%s: shown and saved differ:\n%s\n---\n%s", name, out.String(), saved)
		}
		if json.Valid(saved) != (name == "r.json") {
			t.Errorf("%s has the wrong form", name)
		}
		if st, _ := os.Stat(path); os.PathSeparator == '/' && st.Mode().Perm() != 0o600 {
			t.Errorf("mode %v", st.Mode())
		}
		if err := report(context.Background(), []string{"--no-net", "--output", path}, io.Discard, io.Discard); err == nil || read(t, path) != string(saved) {
			t.Error("an existing file must not be overwritten", err)
		}
	}
	var out bytes.Buffer
	if err := report(context.Background(), []string{"--json", "--no-net"}, &out, io.Discard); err != nil || !json.Valid(out.Bytes()) {
		t.Error("--json", err)
	}
	if err := report(context.Background(), []string{"report.json"}, io.Discard, io.Discard); err == nil {
		t.Error("a stray argument must be refused")
	}
	if err := report(context.Background(), []string{"--output", filepath.Join(dir, "no", "dir.json"), "--no-net"}, io.Discard, io.Discard); err == nil {
		t.Error("a file that cannot be written must be reported")
	}
}

func TestNoNetTestsNoSite(t *testing.T) {
	asked := make(chan string, 100)
	old := siteTester
	siteTester = func(_ context.Context, host string, _ bool, _ platformData) siteReport {
		asked <- host
		return siteReport{Host: host}
	}
	defer func() { siteTester = old }()
	if err := report(context.Background(), []string{"--no-net"}, io.Discard, io.Discard); err != nil || len(asked) != 0 {
		t.Fatal("--no-net must not touch the network", err, len(asked))
	}
	if err := report(context.Background(), nil, io.Discard, io.Discard); err != nil || len(asked) != len(reportHosts) {
		t.Fatal("every site once", err, len(asked))
	}
}

func TestHandshakeThroughProxy(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	p := &proxy{tlsPort: port, cache: map[string]cached{}, hosts: newHostlist("", ""),
		resolve: func(context.Context, string) []net.IP { return []net.IP{net.ParseIP("127.0.0.1")} }}
	go p.serve(ln)
	if got := handshakeVia(context.Background(), ln.Addr().String(), "example.com", port); !reached(got) {
		t.Error("through the proxy:", got)
	}
	ln.Close()
	if got := handshakeVia(context.Background(), ln.Addr().String(), "example.com", port); got != "tcp-fail" {
		t.Error("without a proxy:", got)
	}
}

func TestProxyKindAndWindowsChecks(t *testing.T) {
	ours, _ := ourConfig(proxyConfig{}, "127.0.0.1:8899")
	for want, c := range map[string]proxyConfig{
		"fragma":       ours,
		"other":        {Flags: flagProxy, Server: "127.0.0.1:3128"},
		"setup_script": {Flags: flagPAC, PAC: "http://x/p.pac"},
		"none":         {Flags: flagDirect, Server: "left.test:1"},
	} {
		if got := proxyKind(c, ""); got != want {
			t.Errorf("%+v: %s, want %s", c, got, want)
		}
	}
	failed := func(running bool, w windowsInfo) string {
		var out []string
		for _, c := range windowsChecks(true, running, w) {
			if c.Status == "failed" {
				out = append(out, c.Code)
			}
		}
		return strings.Join(out, ",")
	}
	left := windowsInfo{SystemProxy: "fragma", StartsAtSignIn: true, AppsEntries: true, RestorePending: true}
	if got := failed(false, left); got != "FR_NOT_RUNNING,FR_PROXY_LEFT,FR_HOSTS_LEFT,FR_RESTORE_PENDING" {
		t.Error(got)
	}
	left.RestorePending = false
	if got := failed(true, left); got != "" {
		t.Error("all of this is normal while running:", got)
	}
}

func TestInstalledVersion(t *testing.T) {
	for in, want := range map[string]string{"fragma 0.4.0\n": "0.4.0", "fragma 0.4.0\r\n": "0.4.0", "alice 1.2.3": "", "fragma 203.0.113.77": "", "": ""} {
		if got := versionOf(in); got != want {
			t.Errorf("%q: %q", in, got)
		}
	}
	if sameVersion(version).Status != "ok" || sameVersion("").Status != "ok" || sameVersion("0.0.1").Code != "FR_VERSION_DIFFERS" {
		t.Error("version check")
	}
}
