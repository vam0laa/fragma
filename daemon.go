//go:build linux

package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
)

const (
	defaultHostsFile = "/etc/fragma/hosts.txt"
	defaultStateDir  = "/var/lib/fragma"
)

// runDaemon is the system-wide mode: it needs root and nftables. It sends the
// HTTPS of every app on this machine through fragma, which changes only the
// connections to blocked hosts and passes everything else through unchanged.
// The nftables rules are removed again when it stops.
func runDaemon(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	tcpPort := fs.Int("tcp-port", defaultTCPPort, "local port for redirected HTTPS")
	dnsPort := fs.Int("dns-port", defaultDNSPort, "local port for redirected DNS")
	dns := fs.Bool("dns", false, "also answer DNS over DNS-over-HTTPS (off by default: nothing is sent to a third party unless you ask)")
	doh := fs.String("doh", "", "comma separated DNS-over-HTTPS URLs for --dns (default: Cloudflare)")
	hostsFile := fs.String("hosts", defaultHostsFile, "file with extra blocked host names, one per line")
	stateDir := fs.String("state", defaultStateDir, "directory where learned hosts are kept")
	fs.Parse(args)

	die := func(format string, a ...any) {
		fmt.Fprintf(os.Stderr, "  ✖ "+format+"\n", a...)
		os.Exit(1)
	}
	if os.Geteuid() != 0 {
		die("system-wide mode needs root: sudo fragma run")
	}
	rs := ruleset(*tcpPort, *dnsPort, *dns)
	if err := checkRules(rs); err != nil {
		die("%v\n(is nftables installed and the kernel built with nf_tables NAT?)", err)
	}
	os.MkdirAll(*stateDir, 0o755)
	hosts := newHostlist(*hostsFile, filepath.Join(*stateDir, "learned.txt"))
	stop := make(chan struct{})
	go hosts.watch(stop)

	dial := markedDialer().DialContext
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *tcpPort))
	if err != nil {
		die("%v", err)
	}
	p := &proxy{tlsPort: "443", cache: map[string]cached{}, dial: dial, hosts: hosts}
	go p.serveTransparent(ln)

	mode := "HTTPS only, DNS untouched"
	if *dns {
		conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: *dnsPort})
		if err != nil {
			die("%v", err)
		}
		ups := defaultDoH
		if *doh != "" {
			ups = strings.Split(*doh, ",")
		}
		go (&dnsForwarder{conn: conn, client: newDoHClient(dial), upstreams: ups}).serve()
		mode = "HTTPS + DNS-over-HTTPS"
	}

	// Rules go in last, only after the listeners are ready.
	if err := applyRules(rs); err != nil {
		die("%v", err)
	}
	fmt.Printf("fragma is on (%s). Only blocked hosts are changed, everything else passes through.\n", mode)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	for s := range sig {
		if s == syscall.SIGHUP {
			hosts.reload()
			continue
		}
		break
	}
	close(stop)
	removeRules()
	ln.Close()
	fmt.Println("fragma is off: rules removed")
}
