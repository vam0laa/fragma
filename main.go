// vamola-dpi probe: finds out HOW a site is blocked on this connection
// (DNS poisoning, IP block, or SNI-based DPI). Needs no root.
package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"text/tabwriter"
	"time"
)

var targets = []string{
	"discord.com", "gateway.discord.gg", "cdn.discordapp.com",
	"youtube.com", "x.com", "instagram.com",
	"wikipedia.org", "example.com", // controls: normally not blocked
}

const stepTimeout = 5 * time.Second

// classify turns a TLS handshake error into a short label.
func classify(err error) string {
	if err == nil {
		return "ok"
	}
	s := err.Error()
	var ne net.Error
	switch {
	case strings.Contains(s, "connection reset"):
		return "reset"
	case errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()):
		return "timeout"
	case strings.Contains(s, "EOF"):
		return "closed"
	case strings.HasPrefix(s, "remote error"):
		return "server-alert" // the real server answered, so the path works
	}
	return "error"
}

// reached reports whether a handshake got an answer from the real server.
func reached(label string) bool { return label == "ok" || label == "server-alert" }

// res is the outcome of repeating one handshake n times.
type res struct {
	ok, n int
	fail  string // the last failure label
}

func (r res) all() bool { return r.n > 0 && r.ok == r.n }

func (r res) String() string {
	switch {
	case r.n == 0:
		return "-"
	case r.all():
		return "ok"
	case r.ok == 0:
		return r.fail
	}
	return fmt.Sprintf("flaky %d/%d", r.ok, r.n)
}

// verdict combines the probe results into one conclusion.
func verdict(tcpOK bool, realSNI, noSNI res) string {
	switch {
	case !tcpOK:
		return "IP/port unreachable"
	case realSNI.all():
		return "reachable"
	case realSNI.ok > 0:
		return fmt.Sprintf("FLAKY (%d/%d ok)", realSNI.ok, realSNI.n)
	case noSNI.all():
		return "SNI-BASED BLOCK (" + realSNI.fail + ")"
	}
	return "blocked or down (" + realSNI.fail + ")"
}

func bogon(ip net.IP) bool {
	return ip.IsPrivate() || ip.IsLoopback() || ip.IsUnspecified() || ip.IsLinkLocalUnicast()
}

// dnsNote compares plain-UDP DNS (can be poisoned) with DNS-over-HTTPS (cannot).
func dnsNote(plain, doh []net.IP) string {
	if len(plain) == 0 {
		return "plain DNS failed"
	}
	for _, ip := range plain {
		if bogon(ip) {
			return "POISONED"
		}
	}
	for _, p := range plain {
		for _, d := range doh {
			if p.Equal(d) {
				return "ok"
			}
		}
	}
	return "differs"
}

func plainDNS(ctx context.Context, host string) []net.IP {
	r := &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{Timeout: stepTimeout}).DialContext(ctx, "udp", "8.8.8.8:53")
	}}
	ips, _ := r.LookupIP(ctx, "ip4", host)
	return ips
}

func dohDNS(ctx context.Context, host string) []net.IP {
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://1.1.1.1/dns-query?name="+host+"&type=A", nil)
	req.Header.Set("Accept", "application/dns-json")
	resp, err := (&http.Client{Timeout: stepTimeout}).Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	var out struct {
		Answer []struct {
			Type int
			Data string
		}
	}
	if json.NewDecoder(resp.Body).Decode(&out) != nil {
		return nil
	}
	var ips []net.IP
	for _, a := range out.Answer {
		if ip := net.ParseIP(a.Data); a.Type == 1 && ip != nil {
			ips = append(ips, ip)
		}
	}
	return ips
}

// handshake does a TLS handshake to ip:443. An empty sni sends no SNI at all.
// Certificates are NOT verified: this only tests whether the path answers.
func handshake(ctx context.Context, ip net.IP, sni string) string {
	return handshakeWith(ctx, ip, sni, nil)
}

// handshakeWith is handshake plus an optional strategy that rewrites how the
// ClientHello is put on the wire (see experiment.go).
func handshakeWith(ctx context.Context, ip net.IP, sni string, st *strategy) string {
	ctx, cancel := context.WithTimeout(ctx, stepTimeout)
	defer cancel()
	raw, err := (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort(ip.String(), "443"))
	if err != nil {
		return "tcp-fail"
	}
	defer raw.Close()
	var conn net.Conn = raw
	if st != nil && st.send != nil {
		conn = &wrapConn{Conn: raw, st: st, host: sni}
	}
	c := tls.Client(conn, &tls.Config{ServerName: sni, InsecureSkipVerify: true, MinVersion: tls.VersionTLS12})
	return classify(c.HandshakeContext(ctx))
}

const attempts = 3

// handshakeN repeats a handshake: a single try is not reliable behind DPI.
func handshakeN(ctx context.Context, ip net.IP, sni string) res {
	r := res{n: attempts}
	for i := 0; i < attempts; i++ {
		if l := handshake(ctx, ip, sni); reached(l) {
			r.ok++
		} else {
			r.fail = l
		}
		time.Sleep(150 * time.Millisecond)
	}
	return r
}

type row struct {
	host, dns, tcp, verdict string
	real, none              res
}

func probe(ctx context.Context, host string) row {
	r := row{host: host}
	plain, doh := plainDNS(ctx, host), dohDNS(ctx, host)
	r.dns = dnsNote(plain, doh)
	if len(doh) == 0 {
		r.tcp, r.verdict = "-", "no DoH answer"
		return r
	}
	ip := doh[0] // the clean IP, so DNS poisoning does not hide the real picture
	if handshake(ctx, ip, host) == "tcp-fail" {
		r.tcp = "FAIL"
		r.verdict = verdict(false, res{}, res{})
		return r
	}
	r.tcp = "ok"
	r.real, r.none = handshakeN(ctx, ip, host), handshakeN(ctx, ip, "")
	r.verdict = verdict(true, r.real, r.none)
	return r
}

// bypassRunning lists known DPI-bypass processes, since they mask the result.
func bypassRunning() []string {
	var found []string
	ents, _ := os.ReadDir("/proc")
	for _, e := range ents {
		b, err := os.ReadFile("/proc/" + e.Name() + "/comm")
		if err != nil {
			continue
		}
		switch n := strings.TrimSpace(string(b)); n {
		case "nfqws", "tpws", "spoofdpi", "ciadpi", "byedpi":
			found = append(found, n)
		}
	}
	return found
}

func main() {
	ctx := context.Background()
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "experiment":
			runExperiment(ctx, os.Args[2:])
			return
		case "proxy":
			runProxy(ctx, os.Args[2:])
			return
		}
	}
	if b := bypassRunning(); len(b) > 0 {
		fmt.Printf("NOTE: %s is running. Results show the already-bypassed state.\n\n", strings.Join(b, ", "))
	} else {
		fmt.Print("No DPI-bypass process detected: results show the raw connection.\n\n")
	}
	rows := make([]row, len(targets))
	var wg sync.WaitGroup
	for i, h := range targets {
		wg.Add(1)
		go func() { defer wg.Done(); rows[i] = probe(ctx, h) }()
	}
	wg.Wait()
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "HOST\tDNS\tTCP\tTLS(real SNI)\tTLS(no SNI)\tVERDICT")
	for _, r := range rows {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", r.host, r.dns, r.tcp, r.real, r.none, r.verdict)
	}
	w.Flush()
}
