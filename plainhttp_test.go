package main

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestPlainHTTPIsForwarded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		io.WriteString(w, r.URL.RequestURI()+" "+string(b)+" "+r.Header.Get("Proxy-Connection"))
	}))
	defer srv.Close()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	p := &proxy{tlsPort: "443", cache: map[string]cached{}}
	go p.serve(ln)

	pu, _ := url.Parse("http://" + ln.Addr().String())
	hc := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{Proxy: http.ProxyURL(pu)}}
	for i := 0; i < 2; i++ {
		resp, err := hc.Post(srv.URL+"/a?b=1", "text/plain", strings.NewReader("body"))
		if err != nil {
			t.Fatal(err)
		}
		got, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if string(got) != "/a?b=1 body " {
			t.Fatalf("got %q", got)
		}
	}
	// a request aimed at the proxy itself must not loop
	resp, err := hc.Get("http://" + ln.Addr().String() + "/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d", resp.StatusCode)
	}
}
