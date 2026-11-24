package main

import (
	"errors"
	"io"
	"net"
	"sync"
	"syscall"
	"time"
)

// Some servers (and the firewalls in front of them) refuse a fragmented
// ClientHello and hang up. For those, fragma retries once without fragmenting
// and remembers the server for a while, so they keep working.
const noFragTTL = 30 * time.Minute

type noFragCache struct {
	mu sync.Mutex
	m  map[string]time.Time
}

func (c *noFragCache) has(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	t, ok := c.m[key]
	if ok && time.Now().After(t) {
		delete(c.m, key)
		return false
	}
	return ok
}

func (c *noFragCache) add(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = map[string]time.Time{}
	}
	c.m[key] = time.Now().Add(noFragTTL)
}

// isHangup is true when the peer closed or reset the connection.
func isHangup(err error) bool {
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, syscall.ECONNRESET)
}

// sendHello writes the client's first TLS record to up, fragmented unless the
// server is known to dislike that. It returns the connection to use from now
// on (a new one after a fallback) and the first bytes the server answered
// with, which the caller must pass on to the client.
func (p *proxy) sendHello(up net.Conn, first []byte, key string, redial func() (net.Conn, error)) (net.Conn, []byte, error) {
	hello := first
	if !p.noFrag.has(key) {
		hello = fragmentClientHello(first)
	}
	if _, err := up.Write(hello); err != nil {
		return up, nil, err
	}
	if len(hello) == len(first) { // not fragmented: nothing to fall back from
		return up, nil, nil
	}

	buf := make([]byte, 16*1024)
	up.SetReadDeadline(time.Now().Add(4 * time.Second))
	n, err := up.Read(buf)
	up.SetReadDeadline(time.Time{})
	if n > 0 {
		return up, buf[:n], nil
	}
	if !isHangup(err) { // a timeout is not proof that fragmenting is the problem
		return up, nil, nil
	}

	// The server hung up on the fragmented ClientHello: retry as it was.
	up.Close()
	up2, err := redial()
	if err != nil {
		return nil, nil, err
	}
	if _, err := up2.Write(first); err != nil {
		up2.Close()
		return nil, nil, err
	}
	up2.SetReadDeadline(time.Now().Add(8 * time.Second))
	n, err = up2.Read(buf)
	up2.SetReadDeadline(time.Time{})
	switch {
	case n > 0:
		p.noFrag.add(key) // only remember it when the plain hello really worked
		return up2, buf[:n], nil
	case isHangup(err):
		up2.Close()
		return nil, nil, errors.New("server closed the connection")
	}
	return up2, nil, nil
}
