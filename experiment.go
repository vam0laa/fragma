package main

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"text/tabwriter"
	"time"
)

// A strategy changes how the TLS ClientHello is written to the socket, to see
// which trick the DPI box on this connection does NOT understand.
type strategy struct {
	name string
	send func(c net.Conn, p []byte, host string) error // nil = plain
}

// wrapConn rewrites the first Write (the ClientHello) with the strategy.
type wrapConn struct {
	net.Conn
	st   *strategy
	host string
	done bool
}

func (w *wrapConn) Write(p []byte) (int, error) {
	if w.done {
		return w.Conn.Write(p)
	}
	w.done = true
	if err := w.st.send(w.Conn, p, w.host); err != nil {
		return 0, err
	}
	return len(p), nil
}

const gap = 20 * time.Millisecond // keeps the two pieces in separate TCP segments

// sniStart is the offset of the host name inside the ClientHello, or -1.
func sniStart(p []byte, host string) int { return bytes.Index(p, []byte(host)) }

func posFirstByte(p []byte, host string) int { return 1 }
func posSNIStart(p []byte, host string) int  { return sniStart(p, host) }
func posMidSNI(p []byte, host string) int {
	if i := sniStart(p, host); i >= 0 {
		return i + len(host)/2
	}
	return -1
}

// tcpSplit writes the ClientHello as two TCP segments cut at pos().
func tcpSplit(pos func([]byte, string) int) func(net.Conn, []byte, string) error {
	return func(c net.Conn, p []byte, host string) error {
		k := pos(p, host)
		if k <= 0 || k >= len(p) {
			_, err := c.Write(p)
			return err
		}
		if _, err := c.Write(p[:k]); err != nil {
			return err
		}
		time.Sleep(gap)
		_, err := c.Write(p[k:])
		return err
	}
}

// fragmentRecord cuts one TLS handshake record into two valid TLS records at
// body offset k. Returns nil when p is not a record we can cut.
func fragmentRecord(p []byte, k int) (r1, r2 []byte) {
	if len(p) < 6 || p[0] != 0x16 || k <= 0 || k >= len(p)-5 {
		return nil, nil
	}
	body := p[5:]
	mk := func(b []byte) []byte {
		r := []byte{p[0], p[1], p[2], byte(len(b) >> 8), byte(len(b))}
		return append(r, b...)
	}
	return mk(body[:k]), mk(body[k:])
}

// recordFrag splits the ClientHello into two TLS records in the middle of the
// SNI. With tcp=true each record also goes out as its own TCP segment.
func recordFrag(tcp bool) func(net.Conn, []byte, string) error {
	return func(c net.Conn, p []byte, host string) error {
		mid := posMidSNI(p, host)
		r1, r2 := fragmentRecord(p, mid-5)
		if r1 == nil {
			_, err := c.Write(p)
			return err
		}
		if !tcp {
			_, err := c.Write(append(r1, r2...))
			return err
		}
		if _, err := c.Write(r1); err != nil {
			return err
		}
		time.Sleep(gap)
		_, err := c.Write(r2)
		return err
	}
}

var strategies = []strategy{
	{"plain", nil},
	{"tcp-split@1", tcpSplit(posFirstByte)},
	{"tcp-split@sni", tcpSplit(posSNIStart)},
	{"tcp-split@mid-sni", tcpSplit(posMidSNI)},
	{"record-frag", recordFrag(false)},
	{"record-frag+split", recordFrag(true)},
}

// runExperiment tries every strategy N times per host and prints success counts.
func runExperiment(ctx context.Context, args []string) {
	hosts := []string{"discord.com", "gateway.discord.gg", "cdn.discordapp.com"}
	if len(args) > 0 {
		hosts = args
	}
	const n = 10
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprint(w, "HOST")
	for _, s := range strategies {
		fmt.Fprint(w, "\t"+s.name)
	}
	fmt.Fprintln(w)
	for _, h := range hosts {
		ips := dohDNS(ctx, h)
		fmt.Fprint(w, h)
		if len(ips) == 0 {
			fmt.Fprintln(w, "\tno DoH answer")
			continue
		}
		for i := range strategies {
			ok := 0
			for j := 0; j < n; j++ {
				if reached(handshakeWith(ctx, ips[0], h, &strategies[i])) {
					ok++
				}
				time.Sleep(100 * time.Millisecond)
			}
			fmt.Fprintf(w, "\t%d/%d", ok, n)
		}
		fmt.Fprintln(w)
	}
	w.Flush()
}
