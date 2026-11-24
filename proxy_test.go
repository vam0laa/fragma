package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// capListener records the first bytes every accepted connection receives.
type capListener struct {
	net.Listener
	mu  sync.Mutex
	got []byte
}

type capConn struct {
	net.Conn
	l *capListener
}

func (l *capListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	return &capConn{c, l}, err
}

func (c *capConn) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	c.l.mu.Lock()
	if len(c.l.got) < 4096 {
		c.l.got = append(c.l.got, b[:n]...)
	}
	c.l.mu.Unlock()
	return n, err
}

func TestHostlist(t *testing.T) {
	h := newHostlist("", "", "Example.ORG", "*.blocked.test")
	for host, want := range map[string]bool{
		"discord.com":          true, // built in
		"cdn.discord.com":      true, // parent domain matches
		"gateway.discord.gg":   true,
		"notdiscord.com":       false, // must not match on a bare suffix
		"discord.com.evil.net": false,
		"example.org":          true, // user host, normalized
		"a.b.example.org":      true,
		"x.blocked.test":       true,
		"turktelekom.com.tr":   false, // everything else is left alone
		"":                     false,
	} {
		if got := h.matches(host); got != want {
			t.Errorf("matches(%q) = %v, want %v", host, got, want)
		}
	}
	var nilList *hostlist
	if nilList.matches("discord.com") {
		t.Error("a nil list must match nothing")
	}
}

func TestParseHosts(t *testing.T) {
	got := parseHosts(strings.NewReader("# comment\n\nFoo.com  # trailing\n*.bar.org\n  baz.net\n"))
	if strings.Join(got, ",") != "foo.com,bar.org,baz.net" {
		t.Fatalf("parseHosts = %v", got)
	}
}

func TestLearnPersistsAndReloads(t *testing.T) {
	dir := t.TempDir()
	user, learned := filepath.Join(dir, "hosts.txt"), filepath.Join(dir, "learned.txt")
	os.WriteFile(user, []byte("one.test\n"), 0o644)
	h := newHostlist(user, learned)
	if !h.matches("one.test") || h.matches("two.test") {
		t.Fatal("user file not read")
	}
	h.learn("Two.Test")
	if !h.matches("two.test") {
		t.Fatal("learned host not matched")
	}
	h2 := newHostlist(user, learned) // a restart
	if !h2.matches("two.test") {
		t.Fatal("learned host must survive a restart")
	}
	os.WriteFile(user, []byte("one.test\nthree.test\n"), 0o644)
	os.Chtimes(user, time.Now().Add(time.Hour), time.Now().Add(time.Hour))
	h.reload()
	if !h.matches("three.test") {
		t.Fatal("changed user file must be picked up")
	}
}

// End to end: the proxy must deliver the ClientHello to a real TLS server as
// two TLS records, and the handshake plus a request must still succeed.
func TestProxyFragmentsAndStillWorks(t *testing.T) {
	cl := &capListener{}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, "hello") }))
	cl.Listener = srv.Listener
	srv.Listener = cl
	srv.StartTLS()
	defer srv.Close()
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())

	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	p := &proxy{
		tlsPort: port,
		hosts:   newHostlist("", "", "blocked.test"),
		cache:   map[string]cached{},
		resolve: func(context.Context, string) []net.IP { return []net.IP{net.ParseIP("127.0.0.1")} },
	}
	go p.serve(ln)

	pu, _ := url.Parse("http://" + ln.Addr().String())
	hc := &http.Client{Transport: &http.Transport{
		Proxy: http.ProxyURL(pu),
		// the test server uses a throw-away certificate; only the path is tested here
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}}
	// "blocked.test" is a fake host name that the stub resolver maps to 127.0.0.1;
	// it is also the SNI the proxy has to hide.
	resp, err := hc.Get("https://blocked.test:" + port + "/")
	if err != nil {
		t.Fatal("request through proxy failed:", err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(b) != "hello" {
		t.Fatalf("body = %q", b)
	}

	cl.mu.Lock()
	got := append([]byte(nil), cl.got...)
	cl.mu.Unlock()
	br := bufio.NewReader(io.NopCloser(newBytesReader(got)))
	r1, err1 := readRecord(br)
	r2, err2 := readRecord(br)
	if err1 != nil || err2 != nil {
		t.Fatal("could not read two records:", err1, err2)
	}
	if r1[0] != 0x16 || r2[0] != 0x16 {
		t.Fatalf("both records must be handshake records, got %x and %x", r1[0], r2[0])
	}
	if r1[5] != 0x01 {
		t.Errorf("first record must start with a ClientHello (0x01), got %x", r1[5])
	}
}

type bytesReader struct {
	b []byte
	i int
}

func newBytesReader(b []byte) *bytesReader { return &bytesReader{b: b} }
func (r *bytesReader) Read(p []byte) (int, error) {
	if r.i >= len(r.b) {
		return 0, io.EOF
	}
	n := copy(p, r.b[r.i:])
	r.i += n
	return n, nil
}

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

func TestDNSForwarder(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "application/dns-message" {
			http.Error(w, "bad type", 400)
			return
		}
		b, _ := io.ReadAll(r.Body)
		w.Write(append(b, 'R'))
	}))
	defer up.Close()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	f := &dnsForwarder{conn: conn, client: up.Client(), upstreams: []string{"http://127.0.0.1:1/dead", up.URL}}
	go f.serve()

	c, _ := net.Dial("udp", conn.LocalAddr().String())
	defer c.Close()
	q := []byte("0123456789ab-query") // >= 12 bytes like a real DNS header
	c.Write(q)
	c.SetReadDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, 100)
	n, err := c.Read(buf)
	if err != nil {
		t.Fatal("no answer:", err)
	}
	if string(buf[:n]) != string(q)+"R" {
		t.Fatalf("answer = %q (first upstream was dead, the second must be used)", buf[:n])
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

func TestBox(t *testing.T) {
	old := colorOn
	defer func() { colorOn = old }()
	for _, on := range []bool{false, true} {
		colorOn = on
		out := box("fragma is ON", []string{"", "short", paint(cBold, "bold part") + "   rest of the line", ""}, cGreen)
		lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
		w := visibleLen(lines[0])
		for _, l := range lines {
			if visibleLen(l) != w {
				t.Fatalf("color=%v: line %q is %d wide, want %d", on, l, visibleLen(l), w)
			}
		}
		if !on && strings.Contains(out, "\x1b[") {
			t.Fatal("no escape codes allowed when color is off")
		}
	}
}

// pickyListener plays a server (or firewall) that hangs up on a fragmented
// ClientHello and counts how often it did.
type pickyListener struct {
	net.Listener
	rejected atomic.Int32
}

type replayConn struct {
	net.Conn
	r io.Reader
}

func (c *replayConn) Read(p []byte) (int, error) { return c.r.Read(p) }

func (l *pickyListener) Accept() (net.Conn, error) {
	for {
		c, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		hdr := make([]byte, 9)
		c.SetReadDeadline(time.Now().Add(2 * time.Second))
		if _, err := io.ReadFull(c, hdr); err != nil {
			c.Close()
			continue
		}
		c.SetReadDeadline(time.Time{})
		recLen := int(hdr[3])<<8 | int(hdr[4])
		hsLen := int(hdr[6])<<16 | int(hdr[7])<<8 | int(hdr[8])
		if hdr[0] == 0x16 && hsLen+4 != recLen { // the ClientHello is split over records
			l.rejected.Add(1)
			c.Close()
			continue
		}
		return &replayConn{Conn: c, r: io.MultiReader(bytes.NewReader(hdr), c)}, nil
	}
}

func TestFallbackForPickyServer(t *testing.T) {
	ln0, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	pl := &pickyListener{Listener: ln0}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, "hello") }))
	srv.Listener.Close()
	srv.Listener = pl
	srv.StartTLS()
	defer srv.Close()
	_, port, _ := net.SplitHostPort(ln0.Addr().String())

	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	p := &proxy{
		tlsPort: port,
		hosts:   newHostlist("", "", "picky.test"),
		cache:   map[string]cached{},
		resolve: func(context.Context, string) []net.IP { return []net.IP{net.ParseIP("127.0.0.1")} },
	}
	go p.serve(ln)
	pu, _ := url.Parse("http://" + ln.Addr().String())
	get := func() string {
		hc := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{
			Proxy:             http.ProxyURL(pu),
			DisableKeepAlives: true,
			TLSClientConfig:   &tls.Config{InsecureSkipVerify: true},
		}}
		resp, err := hc.Get("https://picky.test:" + port + "/")
		if err != nil {
			t.Fatal("request failed:", err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return string(b)
	}

	if got := get(); got != "hello" {
		t.Fatalf("first request: %q", got)
	}
	if n := pl.rejected.Load(); n != 1 {
		t.Fatalf("the fragmented attempt should have been refused once, got %d", n)
	}
	if got := get(); got != "hello" {
		t.Fatalf("second request: %q", got)
	}
	if n := pl.rejected.Load(); n != 1 {
		t.Fatalf("the server must be remembered: expected still 1 refusal, got %d", n)
	}
}

func TestNoFragCacheExpires(t *testing.T) {
	var c noFragCache
	if c.has("a") {
		t.Fatal("empty cache")
	}
	c.add("a")
	if !c.has("a") || c.has("b") {
		t.Fatal("add/has")
	}
	c.mu.Lock()
	c.m["a"] = time.Now().Add(-time.Second)
	c.mu.Unlock()
	if c.has("a") {
		t.Fatal("entry must expire")
	}
}

// dpiListener plays a blocking DPI box: it resets every connection whose
// plain (unfragmented) ClientHello names the blocked host.
type dpiListener struct {
	net.Listener
	blocked string
	resets  atomic.Int32
}

func (l *dpiListener) Accept() (net.Conn, error) {
	for {
		c, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		c.SetReadDeadline(time.Now().Add(2 * time.Second))
		rec, err := readRecord(c)
		if err != nil {
			c.Close()
			continue
		}
		c.SetReadDeadline(time.Time{})
		if host, _, ok := findSNI(rec); ok && host == l.blocked {
			l.resets.Add(1)
			c.(*net.TCPConn).SetLinger(0) // close sends a RST
			c.Close()
			continue
		}
		return &replayConn{Conn: c, r: io.MultiReader(bytes.NewReader(rec), c)}, nil
	}
}

// startTLS runs an HTTPS test server on a wrapped listener and a proxy in front.
func startProxyAndServer(t *testing.T, wrap func(net.Listener) net.Listener, hosts *hostlist) (port string, p *proxy, get func(host string) (string, error)) {
	t.Helper()
	ln0, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, "hello") }))
	srv.Listener.Close()
	srv.Listener = wrap(ln0)
	srv.StartTLS()
	t.Cleanup(srv.Close)
	_, port, _ = net.SplitHostPort(ln0.Addr().String())

	pln, _ := net.Listen("tcp", "127.0.0.1:0")
	t.Cleanup(func() { pln.Close() })
	p = &proxy{
		tlsPort: port,
		hosts:   hosts,
		cache:   map[string]cached{},
		resolve: func(context.Context, string) []net.IP { return []net.IP{net.ParseIP("127.0.0.1")} },
	}
	go p.serve(pln)
	pu, _ := url.Parse("http://" + pln.Addr().String())
	get = func(host string) (string, error) {
		hc := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{
			Proxy: http.ProxyURL(pu), DisableKeepAlives: true,
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}}
		resp, err := hc.Get("https://" + host + ":" + port + "/")
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return string(b), nil
	}
	return
}

// The core promise: a host that is not on any list is passed through unchanged.
func TestUnlistedHostIsUntouched(t *testing.T) {
	var cl *capListener
	_, _, get := startProxyAndServer(t, func(l net.Listener) net.Listener { cl = &capListener{Listener: l}; return cl }, newHostlist("", ""))
	if got, err := get("turktelekom.test"); err != nil || got != "hello" {
		t.Fatalf("request: %q %v", got, err)
	}
	if n := cl.handshakeRecordsForTest(); n != 1 {
		t.Fatalf("an unlisted host must get its ClientHello unchanged (1 record), got %d records", n)
	}
}

// A host that is cut when plain but works fragmented is learned, once.
func TestBlockedHostIsLearned(t *testing.T) {
	var dpi *dpiListener
	hosts := newHostlist("", filepath.Join(t.TempDir(), "learned.txt"))
	_, _, get := startProxyAndServer(t, func(l net.Listener) net.Listener { dpi = &dpiListener{Listener: l, blocked: "reset.test"}; return dpi }, hosts)

	if hosts.matches("reset.test") {
		t.Fatal("must not be known beforehand")
	}
	if got, err := get("reset.test"); err != nil || got != "hello" {
		t.Fatalf("first request must succeed through the automatic retry: %q %v", got, err)
	}
	if n := dpi.resets.Load(); n != 1 {
		t.Fatalf("expected exactly 1 reset of the plain attempt, got %d", n)
	}
	if !hosts.matches("reset.test") {
		t.Fatal("the blocked host must now be learned")
	}
	if got, err := get("reset.test"); err != nil || got != "hello" {
		t.Fatalf("second request: %q %v", got, err)
	}
	if n := dpi.resets.Load(); n != 1 {
		t.Fatalf("a learned host must be fragmented right away: resets went to %d", n)
	}
	// an unrelated host on the same server stays untouched
	if got, err := get("fine.test"); err != nil || got != "hello" {
		t.Fatalf("other host: %q %v", got, err)
	}
	if n := dpi.resets.Load(); n != 1 {
		t.Fatalf("other host must not trigger the DPI: %d", n)
	}
}

func (l *capListener) handshakeRecordsForTest() int {
	l.mu.Lock()
	b := append([]byte(nil), l.got...)
	l.mu.Unlock()
	n := 0
	for len(b) >= 5 && b[0] == 0x16 {
		n++
		ln := 5 + (int(b[3])<<8 | int(b[4]))
		if ln > len(b) {
			break
		}
		b = b[ln:]
	}
	return n
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
