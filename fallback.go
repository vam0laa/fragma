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
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, syscall.ECONNRESET) {
		return true
	}
	var en syscall.Errno
	return errors.As(err, &en) && (en == 10054 || en == 10053) // Windows: WSAECONNRESET, WSAECONNABORTED
}

// sendHello writes the client's first TLS record to up and returns the
// connection to use from now on plus the first bytes the server answered with
// (the caller passes them on to the client).
//
// Only hosts that need it get a fragmented ClientHello:
//   - a host on a list is fragmented; if the server hangs up on that, retry plain
//     and remember the server (some refuse fragmented hellos)
//   - any other host is sent unchanged; if the server hangs up (the typical
//     sign of a block), retry fragmented and, if that works, learn the host
func (p *proxy) sendHello(up net.Conn, first []byte, key string, redial func() (net.Conn, error)) (net.Conn, []byte, error) {
	host, _, hasSNI := findSNI(first)
	frag := fragmentClientHello(first)
	listed := hasSNI && p.hosts.matches(host)

	type attempt struct {
		hello   []byte
		fragged bool
	}
	var plan []attempt
	switch {
	case !hasSNI || len(frag) == len(first), p.noFrag.has(key):
		plan = []attempt{{first, false}}
	case listed:
		plan = []attempt{{frag, true}, {first, false}}
	default:
		plan = []attempt{{first, false}, {frag, true}}
	}

	buf := make([]byte, 16*1024)
	conn := up
	for i, a := range plan {
		if i > 0 {
			c, err := redial()
			if err != nil {
				return nil, nil, err
			}
			conn = c
		}
		if _, err := conn.Write(a.hello); err != nil {
			conn.Close()
			return nil, nil, err
		}
		if len(plan) == 1 { // nothing to compare against: just pipe
			return conn, nil, nil
		}
		conn.SetReadDeadline(time.Now().Add(4 * time.Second))
		n, err := conn.Read(buf)
		conn.SetReadDeadline(time.Time{})
		if n > 0 {
			switch {
			case a.fragged && !listed:
				p.hosts.learn(host) // plain was cut, fragmented got through: it is blocked
			case !a.fragged && listed:
				p.noFrag.add(key) // this server refuses fragmented hellos
			}
			return conn, buf[:n], nil
		}
		if !isHangup(err) { // a timeout is no proof of anything: carry on
			return conn, nil, nil
		}
		conn.Close()
	}
	return nil, nil, errors.New("server closed the connection")
}
