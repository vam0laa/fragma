package main

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// forwardHTTP passes a plain HTTP proxy request on, unchanged apart from the
// proxy headers. Needed because the Windows proxy setting covers HTTP too: some
// programs (the Discord updater) cannot read a setting that is HTTPS only.
// One request per connection, unless it upgrades (ws://).
func (p *proxy) forwardHTTP(client net.Conn, br *bufio.Reader, req *http.Request) {
	// a request that is not addressed to another server (including one that
	// would come back to us) is refused
	if req.URL.Scheme != "http" || req.URL.Hostname() == "" {
		io.WriteString(client, "HTTP/1.1 400 Bad Request\r\nConnection: close\r\n\r\n")
		return
	}
	port := req.URL.Port()
	if port == "" {
		port = "80"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var up net.Conn
	var err error
	for i, ip := range p.lookup(ctx, req.URL.Hostname()) {
		if i == 3 {
			break
		}
		if up, err = p.dialContext(ctx, net.JoinHostPort(ip.String(), port)); err == nil {
			break
		}
	}
	if up == nil || err != nil {
		io.WriteString(client, "HTTP/1.1 502 Bad Gateway\r\nConnection: close\r\n\r\n")
		return
	}
	defer up.Close()

	upgrade := req.Header.Get("Upgrade") != ""
	req.Header.Del("Proxy-Connection")
	req.Header.Del("Proxy-Authorization")
	if !upgrade {
		req.Close = true
		req.Header.Del("Connection")
	}
	if _, ok := req.Header["User-Agent"]; !ok {
		req.Header.Set("User-Agent", "") // do not add one of our own
	}
	client.SetReadDeadline(time.Time{}) // uploads may take long
	if strings.EqualFold(req.Header.Get("Expect"), "100-continue") {
		req.Header.Del("Expect")
		io.WriteString(client, "HTTP/1.1 100 Continue\r\n\r\n")
	}
	if err := req.Write(up); err != nil {
		return
	}
	if upgrade {
		pipe(client, br, up)
		return
	}
	io.Copy(client, up)
}
