package main

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"time"
)

// One provider by default: a fallback would send your lookups to a second
// company. Choose your own list with --doh.
var defaultDoH = []string{"https://1.1.1.1/dns-query"}

// dnsForwarder answers plain UDP DNS queries by asking a DNS-over-HTTPS server
// (RFC 8484, binary POST). The query is passed through untouched, so no DNS
// library is needed. This defeats DNS poisoning and DNS blocking by the ISP.
type dnsForwarder struct {
	conn      *net.UDPConn
	client    *http.Client
	upstreams []string
}

func (d *dnsForwarder) query(q []byte) []byte {
	for _, u := range d.upstreams {
		req, err := http.NewRequest(http.MethodPost, u, bytes.NewReader(q))
		if err != nil {
			continue
		}
		req.Header.Set("Content-Type", "application/dns-message")
		req.Header.Set("Accept", "application/dns-message")
		resp, err := d.client.Do(req)
		if err != nil {
			continue
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 65535))
		resp.Body.Close()
		if err == nil && resp.StatusCode == http.StatusOK && len(body) >= 12 {
			return body
		}
	}
	return nil
}

func (d *dnsForwarder) serve() {
	buf := make([]byte, 4096)
	for {
		n, addr, err := d.conn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		q := append([]byte(nil), buf[:n]...)
		go func() {
			if resp := d.query(q); resp != nil {
				d.conn.WriteToUDP(resp, addr)
			}
		}()
	}
}

func newDoHClient(dial func(ctx context.Context, network, addr string) (net.Conn, error)) *http.Client {
	return &http.Client{
		Timeout:   5 * time.Second,
		Transport: &http.Transport{DialContext: dial, ForceAttemptHTTP2: true},
	}
}
