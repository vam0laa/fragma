//go:build e2e

package main

// End-to-end test of the system-wide mode. It must run as (fake) root inside a
// private network namespace, so it never touches the real network:
//
//	make e2e
//
// Fake servers play "a blocked site", "an innocent site", "a LAN device", "a
// server that refuses fragmented hellos", "a DPI box" and "the ISP resolver";
// the real fragma binary runs between them and the client.

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func (l *capListener) reset() {
	l.mu.Lock()
	l.got = nil
	l.mu.Unlock()
}

// handshakeRecords counts the leading TLS handshake records the server received.
func (l *capListener) handshakeRecords() int { return l.handshakeRecordsForTest() }

func httpsOn(t *testing.T, addr string, wrap func(net.Listener) net.Listener) {
	t.Helper()
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, "ok") }))
	srv.Listener.Close()
	srv.Listener = wrap(ln)
	srv.StartTLS()
	t.Cleanup(srv.Close)
}

func capServer(t *testing.T, addr string) *capListener {
	var cl *capListener
	httpsOn(t, addr, func(l net.Listener) net.Listener { cl = &capListener{Listener: l}; return cl })
	return cl
}

func dialTLS(t *testing.T, addr, sni string) {
	t.Helper()
	c, err := tls.DialWithDialer(&net.Dialer{Timeout: 5 * time.Second}, "tcp", addr,
		&tls.Config{ServerName: sni, InsecureSkipVerify: true})
	if err != nil {
		t.Fatalf("TLS to %s (SNI %s) failed: %v", addr, sni, err)
	}
	c.Close()
	time.Sleep(100 * time.Millisecond)
}

func askDNS(t *testing.T, addr string) string {
	t.Helper()
	c, err := net.Dial("udp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.Write([]byte("0123456789ab-query"))
	c.SetReadDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, 200)
	n, err := c.Read(buf)
	if err != nil {
		t.Fatalf("no DNS answer from %s: %v", addr, err)
	}
	return string(buf[:n])
}

func tableExists() bool { return exec.Command("nft", "list", "table", "ip", "fragma").Run() == nil }

var daemonPid int

// openFiles counts what the running daemon holds open.
func openFiles(t *testing.T) int {
	t.Helper()
	f, err := os.ReadDir(fmt.Sprintf("/proc/%d/fd", daemonPid))
	if err != nil {
		t.Fatal(err)
	}
	return len(f)
}

// dohName answers an A query for doh.test with 203.0.113.9; nil for any other query.
func dohName(q []byte) []byte {
	name := []byte("\x03doh\x04test\x00")
	if len(q) < 12+len(name)+4 || !bytes.Equal(q[12:12+len(name)], name) {
		return nil
	}
	end := 12 + len(name) + 4
	r := append([]byte{q[0], q[1], 0x81, 0x80, 0, 1, 0, 0, 0, 0, 0, 0}, q[12:end]...)
	if q[end-3] == 1 { // type A
		r[7] = 1
		r = append(r, 0xc0, 0x0c, 0, 1, 0, 1, 0, 0, 0, 60, 0, 4, 203, 0, 113, 9)
	}
	return r
}

// startDaemon runs the real binary and returns a function that stops it and
// checks that the rules are gone.
func startDaemon(t *testing.T, bin string, args ...string) (stop func()) {
	t.Helper()
	cmd := exec.Command(bin, append([]string{"run"}, args...)...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	t.Cleanup(func() { cmd.Process.Kill() })
	daemonPid = cmd.Process.Pid
	for i := 0; i < 50 && !tableExists(); i++ {
		time.Sleep(100 * time.Millisecond)
	}
	if !tableExists() {
		t.Fatal("nft table never appeared")
	}
	return func() {
		cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("daemon did not stop")
		}
		if tableExists() {
			t.Fatal("nft table still present after stop")
		}
	}
}

func TestSandbox(t *testing.T) {
	bin := os.Getenv("FRAGMA_BIN")
	if bin == "" || os.Geteuid() != 0 {
		t.Skip("run with: make e2e")
	}
	tmp := t.TempDir()
	hostsFile := filepath.Join(tmp, "hosts.txt")
	stateDir := filepath.Join(tmp, "state")
	os.WriteFile(hostsFile, []byte("blocked.test\npicky.test\n"), 0o644)

	site := capServer(t, "203.0.113.5:443") // serves "blocked.test" and "fine.test"
	lan := capServer(t, "10.1.2.3:443")
	var picky *pickyListener
	httpsOn(t, "203.0.113.7:443", func(l net.Listener) net.Listener { picky = &pickyListener{Listener: l}; return picky })
	var dpi *dpiListener
	httpsOn(t, "203.0.113.8:443", func(l net.Listener) net.Listener {
		dpi = &dpiListener{Listener: l, blocked: "reset.test"}
		return dpi
	})

	isp, err := net.ListenPacket("udp", "203.0.113.53:53")
	if err != nil {
		t.Fatal(err)
	}
	defer isp.Close()
	go func() { // the ISP resolver: always lies
		buf := make([]byte, 512)
		for {
			n, a, err := isp.ReadFrom(buf)
			if err != nil {
				return
			}
			if r := dohName(buf[:n]); r != nil { // the truth about one name
				isp.WriteTo(r, a)
				continue
			}
			isp.WriteTo([]byte("ISP-LIES-0123456"), a)
		}
	}()
	var dohHits atomic.Int32
	doh := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dohHits.Add(1)
		b, _ := io.ReadAll(r.Body)
		w.Write(append(b, 'R'))
	}))
	doh.Listener.Close()
	dl, err := net.Listen("tcp", "127.0.0.1:19999")
	if err != nil {
		t.Fatal(err)
	}
	doh.Listener = dl
	doh.Start()
	defer doh.Close()

	// 0. without fragma everything is direct
	dialTLS(t, "203.0.113.5:443", "blocked.test")
	if n := site.handshakeRecords(); n != 1 {
		t.Fatalf("before fragma: expected 1 handshake record, got %d", n)
	}

	// ---- phase A: default settings (DNS untouched) ----
	stop := startDaemon(t, bin, "--hosts", hostsFile, "--state", stateDir)

	// 1. a listed host is fragmented
	site.reset()
	dialTLS(t, "203.0.113.5:443", "blocked.test")
	if n := site.handshakeRecords(); n != 2 {
		t.Fatalf("listed host: expected the ClientHello in 2 records, got %d", n)
	}
	// 2. THE promise: a host that is not blocked is passed through untouched,
	//    even on the very same server and address
	site.reset()
	dialTLS(t, "203.0.113.5:443", "fine.test")
	if n := site.handshakeRecords(); n != 1 {
		t.Fatalf("unlisted host must be left alone (1 record), got %d", n)
	}
	// 3. LAN addresses are never redirected
	dialTLS(t, "10.1.2.3:443", "blocked.test")
	if n := lan.handshakeRecords(); n != 1 {
		t.Fatalf("private address must not be touched, got %d records", n)
	}
	// 4. a listed host whose server refuses fragmented hellos still works, and is remembered
	dialTLS(t, "203.0.113.7:443", "picky.test")
	dialTLS(t, "203.0.113.7:443", "picky.test")
	if n := picky.rejected.Load(); n != 1 {
		t.Fatalf("picky server: expected exactly 1 refusal, got %d", n)
	}
	// 5. an unlisted host that the DPI box cuts is noticed, fixed and learned
	dialTLS(t, "203.0.113.8:443", "reset.test")
	dialTLS(t, "203.0.113.8:443", "reset.test")
	if n := dpi.resets.Load(); n != 1 {
		t.Fatalf("DPI box: expected the plain attempt to be cut exactly once, got %d", n)
	}
	b, _ := os.ReadFile(filepath.Join(stateDir, "learned.txt"))
	if !strings.Contains(string(b), "reset.test") {
		t.Fatalf("reset.test must be written to the learned file, got %q", b)
	}
	// 6. DNS is NOT touched by default and nothing is sent to a DoH server
	if got := askDNS(t, "203.0.113.53:53"); got != "ISP-LIES-0123456" {
		t.Fatalf("DNS must be left alone by default, got %q", got)
	}
	if n := dohHits.Load(); n != 0 {
		t.Fatalf("by default nothing may be sent to a DNS-over-HTTPS server, got %d requests", n)
	}
	// 7. a connection made straight to fragma's own port is dropped, not relayed
	//    back to itself over and over
	before := openFiles(t)
	self, err := net.Dial("tcp", "127.0.0.1:18443")
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	if n := openFiles(t); n > before+3 {
		t.Errorf("a direct connection to fragma's port: open files went from %d to %d", before, n)
	}
	self.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := self.Read(make([]byte, 1)); err != io.EOF {
		t.Errorf("a direct connection to fragma's port must be closed, got %v", err)
	}
	self.Close()
	stop()
	site.reset()
	dialTLS(t, "203.0.113.5:443", "blocked.test")
	if n := site.handshakeRecords(); n != 1 {
		t.Fatalf("after stop: expected direct traffic, got %d records", n)
	}

	// ---- phase B: DNS-over-HTTPS only when asked for ----
	stop = startDaemon(t, bin, "--hosts", hostsFile, "--state", stateDir, "--dns", "--doh", "http://127.0.0.1:19999/dns-query")
	if got := askDNS(t, "203.0.113.53:53"); got != "0123456789ab-queryR" {
		t.Fatalf("with --dns the lookup must go over DoH, got %q", got)
	}
	stop()
	if got := askDNS(t, "203.0.113.53:53"); got != "ISP-LIES-0123456" {
		t.Fatalf("after stop the ISP answers again, got %q", got)
	}

	// ---- phase C: a DNS-over-HTTPS server given by name ----
	// The system's resolver (see Makefile) is the ISP's. fragma's own lookup of
	// the name must not be redirected back to fragma.
	named := httptest.NewUnstartedServer(doh.Config.Handler)
	named.Listener.Close()
	if named.Listener, err = net.Listen("tcp", "203.0.113.9:19999"); err != nil {
		t.Fatal(err)
	}
	named.Start()
	defer named.Close()
	stop = startDaemon(t, bin, "--hosts", hostsFile, "--state", stateDir, "--dns", "--doh", "http://doh.test:19999/dns-query")
	if got := askDNS(t, "203.0.113.53:53"); got != "0123456789ab-queryR" {
		t.Fatalf("with a DoH server given by name the lookup must go over it, got %q", got)
	}
	stop()

	// ---- phase D: the same behind a local stub resolver (like systemd-resolved) ----
	// What the stub sends on is redirected to fragma like any other DNS.
	stub, err := net.ListenPacket("udp", "127.0.0.53:53")
	if err != nil {
		t.Fatal(err)
	}
	defer stub.Close()
	go func() {
		buf := make([]byte, 1500)
		for {
			n, from, err := stub.ReadFrom(buf)
			if err != nil {
				return
			}
			go func(q []byte) {
				up, err := net.Dial("udp", "203.0.113.53:53")
				if err != nil {
					return
				}
				defer up.Close()
				up.Write(q)
				up.SetReadDeadline(time.Now().Add(2 * time.Second))
				a := make([]byte, 1500)
				if n, err := up.Read(a); err == nil {
					stub.WriteTo(a[:n], from)
				}
			}(append([]byte(nil), buf[:n]...))
		}
	}()
	if err := os.WriteFile("/etc/resolv.conf", []byte("nameserver 127.0.0.53\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stop = startDaemon(t, bin, "--hosts", hostsFile, "--state", stateDir, "--dns", "--doh", "http://doh.test:19999/dns-query")
	for i := 0; i < 3; i++ {
		if got := askDNS(t, "203.0.113.53:53"); got != "0123456789ab-queryR" {
			t.Fatalf("behind a stub resolver the lookup must go over DoH, got %q", got)
		}
	}
	stop()

	// ---- phase E: a server name that cannot be looked up leaves DNS alone ----
	cmd := exec.Command(bin, "run", "--hosts", hostsFile, "--state", stateDir, "--dns", "--doh", "http://no-such-name.test/dns-query")
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "no-such-name.test") || tableExists() {
		t.Fatalf("fragma must refuse to start and set no rules: %v %s", err, out)
	}
}
