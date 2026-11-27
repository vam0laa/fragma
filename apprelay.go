package main

import (
	"bufio"
	"context"
	"net"
	"strings"
	"sync"
	"time"
)

// Some programs ignore the system proxy (the Discord updater is one). For their
// host names the hosts file can point at this machine, where fragma relays the
// TLS connection: it reads the name from the ClientHello, looks up the real
// address over DNS-over-HTTPS and forwards the connection, fragmented.

// appHosts are the names that get a hosts file entry. The hosts file has no
// wildcards, so each one is spelled out.
var appHosts = []string{
	"updates.discord.com", "discord.com", "gateway.discord.gg",
	"cdn.discordapp.com", "media.discordapp.net", "dl.discordapp.net",
	"status.discord.com", "remote-auth-gateway.discord.gg",
	"roblox.com", "www.roblox.com", "web.roblox.com", "apis.roblox.com", "auth.roblox.com",
	"users.roblox.com", "games.roblox.com", "gamejoin.roblox.com", "setup.roblox.com",
	"clientsettings.roblox.com", "clientsettingscdn.roblox.com", "assetdelivery.roblox.com",
	"thumbnails.roblox.com", "presence.roblox.com", "realtime-signalr.roblox.com",
	"ecsv2.roblox.com", "locale.roblox.com", "accountsettings.roblox.com", "catalog.roblox.com",
	"economy.roblox.com", "friends.roblox.com", "groups.roblox.com", "avatar.roblox.com",
	"voice.roblox.com", "ephemeralcounters.api.roblox.com", "tracing.roblox.com",
	"usermoderation.roblox.com", "gameinternationalization.roblox.com", "notifications.roblox.com",
	"privatemessages.roblox.com", "lms.roblox.com", "followings.roblox.com", "contacts.roblox.com",
	"metrics.roblox.com", "inventory.roblox.com", "badges.roblox.com", "chat.roblox.com",
	"premiumfeatures.roblox.com", "textfilter.roblox.com", "trades.roblox.com", "develop.roblox.com"}

// cachedResolver remembers answers for ten minutes, so that only the first
// connection to a name waits for the lookup. Empty answers are not kept.
func cachedResolver(resolve func(context.Context, string) []net.IP) func(context.Context, string) []net.IP {
	var mu sync.Mutex
	m := map[string]cached{}
	return func(ctx context.Context, host string) []net.IP {
		mu.Lock()
		c, ok := m[host]
		mu.Unlock()
		if ok && time.Since(c.at) < 10*time.Minute {
			return c.ips
		}
		ips := resolve(ctx, host)
		if len(ips) > 0 {
			mu.Lock()
			m[host] = cached{ips, time.Now()}
			mu.Unlock()
		}
		return ips
	}
}

func isAppHost(host string) bool {
	host = normalizeHost(host)
	for _, h := range appHosts {
		if h == host {
			return true
		}
	}
	return false
}

const (
	hostsBegin = "# fragma begin (remove with: fragma apps off)"
	hostsEnd   = "# fragma end"
)

// editHosts returns the hosts file content with fragma's block added (on) or
// removed. Every other line is kept as it is.
func editHosts(content string, on bool) string {
	nl := "\n"
	if strings.Contains(content, "\r\n") {
		nl = "\r\n"
	}
	var out strings.Builder
	var held strings.Builder // lines after a begin marker, until its end marker
	inside := false
	for _, line := range strings.SplitAfter(content, "\n") {
		switch t := strings.TrimSpace(line); {
		case t == hostsBegin:
			inside = true
		case inside && t == hostsEnd:
			inside = false
			held.Reset()
		case inside:
			held.WriteString(line)
		default:
			out.WriteString(line)
		}
	}
	out.WriteString(held.String()) // no end marker: do not throw lines away
	s := out.String()
	if !on {
		return s
	}
	if s != "" && !strings.HasSuffix(s, "\n") {
		s += nl
	}
	s += hostsBegin + nl
	for _, h := range appHosts {
		s += "127.0.0.1 " + h + nl + "::1 " + h + nl
	}
	return s + hostsEnd + nl
}

// handleDirect serves a program that connected to us believing we are the
// server. Only the names in appHosts are relayed.
func (p *proxy) handleDirect(client net.Conn) {
	defer client.Close()
	br := bufio.NewReader(client)
	client.SetReadDeadline(time.Now().Add(10 * time.Second))
	first, err := readRecord(br)
	if err != nil {
		return
	}
	host, _, ok := findSNI(first)
	if !ok || !isAppHost(host) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var up net.Conn
	var addr string
	// p.resolve, never the system resolver: that one now answers with our own address.
	for i, ip := range p.resolve(ctx, normalizeHost(host)) {
		if i == 3 {
			break
		}
		addr = net.JoinHostPort(ip.String(), p.tlsPort)
		if up, err = p.dialContext(ctx, addr); err == nil {
			break
		}
	}
	if up == nil || err != nil {
		return
	}
	up, resp, err := p.sendHello(up, first, addr, func() (net.Conn, error) { return p.dialContext(ctx, addr) })
	if err != nil {
		return
	}
	defer up.Close()
	if len(resp) > 0 {
		if _, err := client.Write(resp); err != nil {
			return
		}
	}
	client.SetReadDeadline(time.Time{})
	pipe(client, br, up)
}

func (p *proxy) serveDirect(ln net.Listener) {
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		go p.handleDirect(c)
	}
}
