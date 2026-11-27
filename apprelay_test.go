package main

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestEditHosts(t *testing.T) {
	orig := "# mine\r\n127.0.0.1 localhost\r\n"
	on := editHosts(orig, true)
	if !strings.HasPrefix(on, orig) || !strings.Contains(on, "127.0.0.1 updates.discord.com\r\n") || !strings.Contains(on, "::1 updates.discord.com\r\n") {
		t.Fatalf("on = %q", on)
	}
	if editHosts(on, true) != on {
		t.Fatal("turning on twice must not add a second block")
	}
	if got := editHosts(on, false); got != orig {
		t.Fatalf("off = %q", got)
	}
	broken := orig + hostsBegin + "\r\n10.0.0.1 keep.test\r\n"
	if got := editHosts(broken, false); !strings.Contains(got, "keep.test") {
		t.Fatal("a block without an end marker must not eat the user's lines")
	}
}

func TestCachedResolver(t *testing.T) {
	calls := 0
	r := cachedResolver(func(_ context.Context, h string) []net.IP {
		calls++
		if h == "empty.test" {
			return nil
		}
		return []net.IP{net.ParseIP("192.0.2.1")}
	})
	for _, h := range []string{"a.test", "a.test", "empty.test", "empty.test"} {
		r(context.Background(), h)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3", calls)
	}
}

func TestDirectRelay(t *testing.T) {
	var cl *capListener
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, "hello") }))
	cl = &capListener{Listener: srv.Listener}
	srv.Listener = cl
	srv.StartTLS()
	defer srv.Close()
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	p := &proxy{
		tlsPort: port,
		hosts:   newHostlist("", ""),
		resolve: func(context.Context, string) []net.IP { return []net.IP{net.ParseIP("127.0.0.1")} },
	}
	go p.serveDirect(ln)

	get := func(name string) error {
		hc := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{
			DisableKeepAlives: true,
			TLSClientConfig:   &tls.Config{ServerName: name, InsecureSkipVerify: true},
		}}
		resp, err := hc.Get("https://" + ln.Addr().String() + "/")
		if err != nil {
			return err
		}
		return resp.Body.Close()
	}
	if get("other.test") == nil {
		t.Fatal("a name that is not on the list must not be relayed")
	}
	if err := get("updates.discord.com"); err != nil {
		t.Fatal(err)
	}
	if n := cl.handshakeRecordsForTest(); n != 2 {
		t.Fatalf("the ClientHello must arrive in 2 records, got %d", n)
	}
}
