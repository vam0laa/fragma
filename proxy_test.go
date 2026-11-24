package main

import (
	"bufio"
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

func TestWants(t *testing.T) {
	p := &proxy{only: []string{"discord.com"}}
	if !p.wants("discord.com") || !p.wants("cdn.discord.com") || p.wants("notdiscord.com") || p.wants("example.org") {
		t.Error("suffix matching wrong")
	}
	if !(&proxy{}).wants("anything.io") {
		t.Error("empty list must match everything")
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
		!strings.Contains(string(b), "ExecStopPost=-/usr/bin/env nft delete table ip fragma") {
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
