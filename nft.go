package main

import (
	"fmt"
	"os/exec"
	"strings"
)

const nftTable = "fragma"

// ruleset builds the nftables rules. Only locally generated traffic is touched
// (the "output" hook). Order matters:
//  1. fragma's own sockets (marked) are never redirected, so there is no loop
//  2. loopback is left alone
//  3. DNS (UDP 53) is redirected, also to LAN resolvers such as the router
//  4. other private ranges are left alone
//  5. HTTPS (TCP 443) is redirected
func ruleset(tcpPort, dnsPort int, dns bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "table ip %s {\n\tchain output {\n\t\ttype nat hook output priority dstnat; policy accept;\n", nftTable)
	fmt.Fprintf(&b, "\t\tmeta mark 0x%x return\n", fwMark)
	b.WriteString("\t\tip daddr 127.0.0.0/8 return\n")
	if dns {
		fmt.Fprintf(&b, "\t\tudp dport 53 redirect to :%d\n", dnsPort)
	}
	b.WriteString("\t\tip daddr { 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16, 169.254.0.0/16 } return\n")
	fmt.Fprintf(&b, "\t\ttcp dport 443 redirect to :%d\n\t}\n}\n", tcpPort)
	return b.String()
}

func nft(stdin string, args ...string) error {
	cmd := exec.Command("nft", args...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("nft %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func applyRules(rs string) error {
	removeRules() // start clean if an old table is left over
	return nft(rs, "-f", "-")
}

func checkRules(rs string) error { return nft(rs, "-c", "-f", "-") }

func removeRules() error { return nft("", "delete", "table", "ip", nftTable) }
