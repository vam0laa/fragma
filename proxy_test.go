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
	"sync"
	"testing"
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

func TestFragmentClientHello(t *testing.T) {
	body := []byte("\x01....discord.com....")
	rec := append([]byte{0x16, 3, 1, 0, byte(len(body))}, body...)
	out := fragmentClientHello(rec, "discord.com")
	if len(out) != len(rec)+5 { // one extra record header
		t.Fatalf("expected two records, total %d bytes (original %d)", len(out), len(rec))
	}
	if got := fragmentClientHello(rec, "other.net"); string(got) != string(rec) {
		t.Error("a record without the SNI must be unchanged")
	}
	if got := fragmentClientHello([]byte{0x17, 3, 3, 0, 2, 1, 2}, "x"); len(got) != 7 {
		t.Error("non-handshake data must be unchanged")
	}
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
