package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// proxy is a local HTTP CONNECT proxy that fragments the TLS ClientHello so
// SNI-based DPI cannot read the host name. It needs no root.
type proxy struct {
	hosts   *hostlist // which hosts get fragmented (nil: none)
	tlsPort string    // only this port is treated as TLS (client speaks first)
	resolve func(ctx context.Context, host string) []net.IP
	dial    func(ctx context.Context, network, addr string) (net.Conn, error) // nil = plain dialer

	noFrag noFragCache // servers that refuse a fragmented ClientHello

	mu    sync.Mutex
	cache map[string]cached
}

type cached struct {
	ips []net.IP
	at  time.Time
}

// lookup resolves through DNS-over-HTTPS first (an ISP cannot poison it),
// falling back to the system resolver. Answers are cached for a minute.
func (p *proxy) lookup(ctx context.Context, host string) []net.IP {
	if ip := net.ParseIP(host); ip != nil {
		return []net.IP{ip}
	}
	p.mu.Lock()
	c, ok := p.cache[host]
	p.mu.Unlock()
	if ok && time.Since(c.at) < time.Minute {
		return c.ips
	}
	ips := p.resolve(ctx, host)
	if len(ips) == 0 {
		ips, _ = net.DefaultResolver.LookupIP(ctx, "ip4", host)
	}
	if len(ips) > 0 {
		p.mu.Lock()
		p.cache[host] = cached{ips, time.Now()}
		p.mu.Unlock()
	}
	return ips
}

func (p *proxy) dialContext(ctx context.Context, addr string) (net.Conn, error) {
	if p.dial != nil {
		return p.dial(ctx, "tcp", addr)
	}
	return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp", addr)
}

// fragmentClientHello cuts a full TLS record (header + body) into two records
// in the middle of the SNI. Anything it cannot cut is returned unchanged.
func fragmentClientHello(rec []byte) []byte {
	host, off, ok := findSNI(rec)
	if !ok {
		return rec
	}
	r1, r2 := fragmentRecord(rec, off+len(host)/2-5)
	if r1 == nil {
		return rec
	}
	return append(r1, r2...)
}

func (p *proxy) handle(client net.Conn) {
	defer client.Close()
	br := bufio.NewReader(client)
	client.SetReadDeadline(time.Now().Add(15 * time.Second))
	req, err := http.ReadRequest(br)
	if err != nil {
		return
	}
	if req.Method != http.MethodConnect {
		io.WriteString(client, "HTTP/1.1 405 Method Not Allowed\r\n\r\nvamola-dpi proxy supports HTTPS (CONNECT) only\r\n")
		return
	}
	host, port, err := net.SplitHostPort(req.Host)
	if err != nil {
		io.WriteString(client, "HTTP/1.1 400 Bad Request\r\n\r\n")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var up net.Conn
	for i, ip := range p.lookup(ctx, host) {
		if i == 3 {
			break
		}
		if up, err = p.dialContext(ctx, net.JoinHostPort(ip.String(), port)); err == nil {
			break
		}
	}
	if up == nil || err != nil {
		io.WriteString(client, "HTTP/1.1 502 Bad Gateway\r\n\r\n")
		return
	}
	defer func() {
		if up != nil {
			up.Close()
		}
	}()
	io.WriteString(client, "HTTP/1.1 200 Connection Established\r\n\r\n")

	// The first record from the client is the ClientHello: rewrite it.
	if port == p.tlsPort {
		first, err := readRecord(br)
		if err != nil {
			return
		}
		addr := up.RemoteAddr().String()
		var resp []byte
		up, resp, err = p.sendHello(up, first, addr, func() (net.Conn, error) { return p.dialContext(ctx, addr) })
		if err != nil {
			return
		}
		if len(resp) > 0 {
			if _, err := client.Write(resp); err != nil {
				return
			}
		}
	}
	client.SetReadDeadline(time.Time{})
	pipe(client, br, up)
}

// readRecord reads one TLS record (5-byte header + body) from r.
func readRecord(r io.Reader) ([]byte, error) {
	hdr := make([]byte, 5)
	if _, err := io.ReadFull(r, hdr); err != nil {
		return nil, err
	}
	n := int(hdr[3])<<8 | int(hdr[4])
	if hdr[0] != 0x16 || n > 16384+256 { // not a handshake: hand the header on as is
		return hdr, nil
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	return append(hdr, body...), nil
}

// pipe copies both directions until both sides are done.
func pipe(client net.Conn, fromClient io.Reader, up net.Conn) {
	var wg sync.WaitGroup
	cp := func(dst net.Conn, src io.Reader) {
		defer wg.Done()
		io.Copy(dst, src)
		if tc, ok := dst.(*net.TCPConn); ok {
			tc.CloseWrite()
		}
	}
	wg.Add(2)
	go cp(up, fromClient)
	go cp(client, up)
	wg.Wait()
}

func (p *proxy) serve(ln net.Listener) {
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		go p.handle(c)
	}
}

func runProxy(ctx context.Context, args []string) {
	fs := flag.NewFlagSet("proxy", flag.ExitOnError)
	listen := fs.String("listen", "127.0.0.1:8899", "loopback address to listen on")
	add := fs.String("add", "", "comma separated extra host names to treat as blocked")
	fs.Parse(args)

	host, _, err := net.SplitHostPort(*listen)
	if ip := net.ParseIP(host); err != nil || ip == nil || !ip.IsLoopback() {
		fmt.Fprintln(os.Stderr, "refusing to listen on a non-loopback address: that would be an open proxy")
		os.Exit(2)
	}
	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	p := &proxy{resolve: dohDNS, cache: map[string]cached{}, tlsPort: "443", hosts: newHostlist("", "", strings.Split(*add, ",")...)}
	fmt.Printf("fragma proxy on %s (HTTPS CONNECT; only blocked hosts are touched)\n", *listen)
	p.serve(ln)
}
