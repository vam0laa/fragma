package main

import (
	"bufio"
	"context"
	"net"
	"strconv"
	"syscall"
	"time"
)

const (
	fwMark         = 0x66726167 // "frag": marks fragma's own sockets so the nft rules skip them
	defaultTCPPort = 18443
	defaultDNSPort = 18053
)

// markedDialer dials with SO_MARK set, so the connection is NOT redirected
// back into fragma. Needs CAP_NET_ADMIN (root).
func markedDialer() *net.Dialer {
	return &net.Dialer{
		Timeout: 5 * time.Second,
		Control: func(_, _ string, c syscall.RawConn) error {
			var serr error
			if err := c.Control(func(fd uintptr) {
				serr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_MARK, fwMark)
			}); err != nil {
				return err
			}
			return serr
		},
	}
}

// handleTransparent serves a connection that nftables redirected to us.
func (p *proxy) handleTransparent(client net.Conn) {
	defer client.Close()
	tc, ok := client.(*net.TCPConn)
	if !ok {
		return
	}
	ip, port, err := originalDst(tc)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	addr := net.JoinHostPort(ip.String(), strconv.Itoa(port))
	up, err := p.dialContext(ctx, addr)
	if err != nil {
		return
	}
	defer func() {
		if up != nil {
			up.Close()
		}
	}()

	br := bufio.NewReader(client)
	if port == 443 { // TLS: the client speaks first, so the first record is the ClientHello
		client.SetReadDeadline(time.Now().Add(10 * time.Second))
		first, err := readRecord(br)
		if err != nil {
			return
		}
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
		client.SetReadDeadline(time.Time{})
	}
	pipe(client, br, up)
}

func (p *proxy) serveTransparent(ln net.Listener) {
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		go p.handleTransparent(c)
	}
}
