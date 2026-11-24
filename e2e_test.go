//go:build e2e

package main

// End-to-end test of the system-wide mode. It must run as (fake) root inside a
// private network namespace, so it never touches the real network:
//
//	make e2e
//
// A TLS server on 203.0.113.5 plays "the blocked site", a UDP server on
// 203.0.113.53 plays "the ISP resolver" and the real fragma binary runs between.

import (
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
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
func (l *capListener) handshakeRecords() int {
	l.mu.Lock()
	b := append([]byte(nil), l.got...)
	l.mu.Unlock()
	n := 0
	for len(b) >= 5 && b[0] == 0x16 {
		n++
		l := 5 + (int(b[3])<<8 | int(b[4]))
		if l > len(b) {
			break
		}
		b = b[l:]
	}
	return n
}

func tlsServer(t *testing.T, addr string) *capListener {
	t.Helper()
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	cl := &capListener{Listener: ln}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, "ok") }))
	srv.Listener.Close()
	srv.Listener = cl
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return cl
}

func dialTLS(t *testing.T, addr string) {
	t.Helper()
	c, err := tls.DialWithDialer(&net.Dialer{Timeout: 3 * time.Second}, "tcp", addr,
		&tls.Config{ServerName: "blocked.test", InsecureSkipVerify: true})
	if err != nil {
		t.Fatalf("TLS to %s failed: %v", addr, err)
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

func TestSandbox(t *testing.T) {
	bin := os.Getenv("FRAGMA_BIN")
	if bin == "" || os.Geteuid() != 0 {
		t.Skip("run with: make e2e")
	}
	blocked := tlsServer(t, "203.0.113.5:443")
	lan := tlsServer(t, "10.1.2.3:443")

	// a server that hangs up on a fragmented ClientHello
	pln, err := net.Listen("tcp", "203.0.113.7:443")
	if err != nil {
		t.Fatal(err)
	}
	picky := &pickyListener{Listener: pln}
	psrv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, "ok") }))
	psrv.Listener.Close()
	psrv.Listener = picky
	psrv.StartTLS()
	defer psrv.Close()

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
			_ = n
			isp.WriteTo([]byte("ISP-LIES-0123456"), a)
		}
	}()
	doh := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

	// 1. before fragma: direct, one handshake record, ISP answers DNS
	dialTLS(t, "203.0.113.5:443")
	if n := blocked.handshakeRecords(); n != 1 {
		t.Fatalf("before fragma: expected 1 handshake record, got %d", n)
	}
	if got := askDNS(t, "203.0.113.53:53"); got != "ISP-LIES-0123456" {
		t.Fatalf("before fragma the ISP must answer, got %q", got)
	}

	// 2. start the real daemon
	cmd := exec.Command(bin, "run", "--doh", "http://127.0.0.1:19999/dns-query")
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	defer cmd.Process.Kill()
	for i := 0; i < 50 && !tableExists(); i++ {
		time.Sleep(100 * time.Millisecond)
	}
	if !tableExists() {
		t.Fatal("nft table never appeared")
	}

	// 3. HTTPS is fragmented (and fragma's own upstream socket is not looped back)
	blocked.reset()
	dialTLS(t, "203.0.113.5:443")
	if n := blocked.handshakeRecords(); n != 2 {
		t.Fatalf("with fragma: expected the ClientHello in 2 records, got %d", n)
	}
	// 4. LAN addresses are left alone
	lan.reset()
	dialTLS(t, "10.1.2.3:443")
	if n := lan.handshakeRecords(); n != 1 {
		t.Fatalf("private address must not be touched, got %d records", n)
	}
	// 4b. a server that refuses fragmented hellos still works (fallback), and is remembered
	dialTLS(t, "203.0.113.7:443")
	if n := picky.rejected.Load(); n != 1 {
		t.Fatalf("picky server: expected exactly 1 refused fragmented attempt, got %d", n)
	}
	dialTLS(t, "203.0.113.7:443")
	if n := picky.rejected.Load(); n != 1 {
		t.Fatalf("picky server must be remembered, refusals went to %d", n)
	}
	// 5. DNS goes to DNS-over-HTTPS, not to the lying ISP
	if got := askDNS(t, "203.0.113.53:53"); got != "0123456789ab-queryR" {
		t.Fatalf("DNS must be answered through DoH, got %q", got)
	}

	// 6. stop: rules are removed, traffic is direct again
	cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("daemon did not stop")
	}
	if tableExists() {
		t.Fatal("nft table still present after stop")
	}
	blocked.reset()
	dialTLS(t, "203.0.113.5:443")
	if n := blocked.handshakeRecords(); n != 1 {
		t.Fatalf("after stop: expected direct traffic (1 record), got %d", n)
	}
	if got := askDNS(t, "203.0.113.53:53"); got != "ISP-LIES-0123456" {
		t.Fatalf("after stop the ISP answers again, got %q", got)
	}
}
