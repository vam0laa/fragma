package main

import (
	"bufio"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// builtinHosts are services that many ISPs block. Everything not on a list is
// left completely alone, unless it turns out to be blocked (then it is learned).
var builtinHosts = []string{
	"discord.com", "discord.gg", "discordapp.com", "discordapp.net", "discord.media", "discordcdn.com",
	"roblox.com", "rbxcdn.com", "robloxlabs.com",
}

const maxLearned = 2000

// hostlist decides which host names get the ClientHello fragmented. Three
// sources: built in, the user's file, and hosts learned by noticing a block.
// A nil *hostlist matches nothing.
type hostlist struct {
	mu          sync.RWMutex
	builtin     map[string]bool
	user        map[string]bool
	learned     map[string]bool
	userFile    string
	learnedFile string
	mod         [2]time.Time
}

func newHostlist(userFile, learnedFile string, extra ...string) *hostlist {
	h := &hostlist{builtin: map[string]bool{}, user: map[string]bool{}, learned: map[string]bool{}, userFile: userFile, learnedFile: learnedFile}
	for _, s := range builtinHosts {
		h.builtin[s] = true
	}
	for _, s := range extra {
		if s = normalizeHost(s); s != "" {
			h.user[s] = true
		}
	}
	h.reload()
	return h
}

func normalizeHost(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimPrefix(s, "*.")
	return strings.Trim(s, ".")
}

// parseHosts reads one host name per line; '#' starts a comment.
func parseHosts(r io.Reader) []string {
	var out []string
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Text()
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		if f := strings.Fields(line); len(f) > 0 {
			if s := normalizeHost(f[0]); s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

// matches reports whether host, or any parent domain of it, is on a list.
func (h *hostlist) matches(host string) bool {
	if h == nil {
		return false
	}
	host = normalizeHost(host)
	h.mu.RLock()
	defer h.mu.RUnlock()
	for host != "" {
		if h.builtin[host] || h.user[host] || h.learned[host] {
			return true
		}
		i := strings.IndexByte(host, '.')
		if i < 0 {
			return false
		}
		host = host[i+1:]
	}
	return false
}

// learn remembers a host that was blocked and got through once fragmented.
func (h *hostlist) learn(host string) {
	if h == nil {
		return
	}
	host = normalizeHost(host)
	h.mu.Lock()
	if host == "" || h.learned[host] || len(h.learned) >= maxLearned {
		h.mu.Unlock()
		return
	}
	h.learned[host] = true
	file := h.learnedFile
	h.mu.Unlock()
	if file == "" {
		return
	}
	if f, err := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
		f.WriteString(host + "\n")
		f.Close()
	}
}

func readHostFile(path string) (map[string]bool, time.Time, bool) {
	st, err := os.Stat(path)
	if path == "" || err != nil {
		return nil, time.Time{}, false
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, time.Time{}, false
	}
	defer f.Close()
	m := map[string]bool{}
	for _, s := range parseHosts(f) {
		m[s] = true
	}
	return m, st.ModTime(), true
}

// reload re-reads the user file and the learned file when they changed.
func (h *hostlist) reload() {
	if h == nil {
		return
	}
	if m, t, ok := readHostFile(h.userFile); ok && !t.Equal(h.mod[0]) {
		h.mu.Lock()
		extra := h.user
		h.user, h.mod[0] = m, t
		for k := range extra { // keep hosts that were given on the command line
			if _, inFile := m[k]; !inFile && h.userFile == "" {
				h.user[k] = true
			}
		}
		h.mu.Unlock()
	}
	if m, t, ok := readHostFile(h.learnedFile); ok && !t.Equal(h.mod[1]) {
		h.mu.Lock()
		for k := range m {
			h.learned[k] = true
		}
		h.mod[1] = t
		h.mu.Unlock()
	}
}

// watch polls the files so `fragma add` takes effect without a restart.
func (h *hostlist) watch(stop <-chan struct{}) {
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			h.reload()
		case <-stop:
			return
		}
	}
}

// lists returns the three sets sorted for display.
func (h *hostlist) lists() (builtin, user, learned []string) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return keys(h.builtin), keys(h.user), keys(h.learned)
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
