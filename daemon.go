package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
)

// runDaemon is the system-wide mode: it needs root and nftables. It redirects
// HTTPS and DNS of every app on this machine through fragma, and removes the
// rules again when it stops.
func runDaemon(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	tcpPort := fs.Int("tcp-port", defaultTCPPort, "local port for redirected HTTPS")
	dnsPort := fs.Int("dns-port", defaultDNSPort, "local port for redirected DNS")
	noDNS := fs.Bool("no-dns", false, "do not touch DNS")
	doh := fs.String("doh", "", "comma separated DNS-over-HTTPS URLs (default: Cloudflare, Quad9, Google)")
	fs.Parse(args)

	die := func(format string, a ...any) {
		fmt.Fprintf(os.Stderr, "fragma: "+format+"\n", a...)
		os.Exit(1)
	}
	if os.Geteuid() != 0 {
		die("system-wide mode needs root: sudo fragma run")
	}
	rs := ruleset(*tcpPort, *dnsPort, !*noDNS)
	if err := checkRules(rs); err != nil {
		die("%v\n(is nftables installed and the kernel built with nf_tables NAT?)", err)
	}

	dial := markedDialer().DialContext
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *tcpPort))
	if err != nil {
		die("%v", err)
	}
	p := &proxy{tlsPort: "443", cache: map[string]cached{}, dial: dial}
	go p.serveTransparent(ln)

	if !*noDNS {
		conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: *dnsPort})
		if err != nil {
			die("%v", err)
		}
		ups := defaultDoH
		if *doh != "" {
			ups = strings.Split(*doh, ",")
		}
		go (&dnsForwarder{conn: conn, client: newDoHClient(dial), upstreams: ups}).serve()
	}

	// Rules go in last, only after both listeners are ready.
	if err := applyRules(rs); err != nil {
		die("%v", err)
	}
	fmt.Printf("fragma is on: HTTPS fragmenting%s for every app on this machine\n", map[bool]string{true: "", false: " + DNS-over-HTTPS"}[*noDNS])

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	removeRules()
	ln.Close()
	fmt.Println("fragma is off: rules removed")
}
