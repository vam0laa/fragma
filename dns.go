package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
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

// pinnedDialer dials DNS-over-HTTPS servers given by name. Once DNS is
// redirected to fragma, looking such a name up can lead back to fragma itself, so
// the addresses are looked up before that and remembered.
type pinnedDialer struct {
	dial   func(ctx context.Context, network, addr string) (net.Conn, error)
	lookup func(ctx context.Context, host string) ([]net.IP, error)

	mu  sync.Mutex
	ips map[string][]net.IP
}

// pin looks up every server of urls that is given by name.
func (p *pinnedDialer) pin(ctx context.Context, urls []string) error {
	for _, s := range urls {
		u, err := url.Parse(s)
		if err != nil || u.Hostname() == "" {
			return fmt.Errorf("not a DNS-over-HTTPS URL: %q", s)
		}
		if host := u.Hostname(); net.ParseIP(host) == nil {
			if _, err := p.refresh(ctx, host); err != nil {
				return fmt.Errorf("cannot look up %s: %w", host, err)
			}
		}
	}
	return nil
}

func (p *pinnedDialer) refresh(ctx context.Context, host string) ([]net.IP, error) {
	ips, err := p.lookup(ctx, host)
	if err == nil && len(ips) == 0 {
		err = errors.New("no address")
	}
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ips == nil {
		p.ips = map[string][]net.IP{}
	}
	p.ips[host] = ips
	return ips, nil
}

// DialContext uses the remembered addresses and asks again only when none of
// them answers.
func (p *pinnedDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil || net.ParseIP(host) != nil {
		return p.dial(ctx, network, addr)
	}
	p.mu.Lock()
	ips := p.ips[host]
	p.mu.Unlock()
	try := func(ips []net.IP) (c net.Conn, err error) {
		err = errors.New("no address for " + host)
		for _, ip := range ips {
			if c, err = p.dial(ctx, network, net.JoinHostPort(ip.String(), port)); err == nil {
				return c, nil
			}
		}
		return nil, err
	}
	c, err := try(ips)
	if err == nil {
		return c, nil
	}
	fresh, lerr := p.refresh(ctx, host)
	if lerr != nil {
		return nil, err
	}
	return try(fresh)
}

func newDoHClient(dial func(ctx context.Context, network, addr string) (net.Conn, error)) *http.Client {
	return &http.Client{
		Timeout:   5 * time.Second,
		Transport: &http.Transport{DialContext: dial, ForceAttemptHTTP2: true},
	}
}
