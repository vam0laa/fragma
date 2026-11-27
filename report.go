// fragma report: collects what is needed to look into a problem and shows it.
// Nothing is ever sent anywhere; the person reads it and decides.
//
// Privacy by construction: a report is made of fixed words, numbers and a few
// version numbers checked against a pattern. No text from this computer (log
// lines, paths, names, addresses) is copied into it.
package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	reportSchema = 1
	reportURL    = "https://github.com/vam0laa/fragma/issues/new?template=report.yml"
)

// The sites tested are fixed and public; the user's own lists never go into a
// report. The last one is normally not blocked anywhere and shows whether the
// network works at all.
var reportHosts = []string{"discord.com", "gateway.discord.gg", "www.roblox.com", "auth.roblox.com", controlHost}

const controlHost = "example.com"

// Report has the same form on every system. What only one system knows is under
// its own key, which is null elsewhere.
type Report struct {
	Schema       int           `json:"schema"`
	Version      string        `json:"fragma_version"`
	Date         string        `json:"date"`
	System       reportSystem  `json:"system"`
	State        reportState   `json:"state"`
	Network      reportNetwork `json:"network"`
	Checks       []reportCheck `json:"checks"`
	FailedChecks []string      `json:"failed_checks"`
	SitesTested  bool          `json:"sites_tested"`
	Sites        []siteReport  `json:"sites"`
	LogStatus    string        `json:"log_status"` // read, none, not_readable
	LogEvents    []logEvent    `json:"log_events"`
	LogUnknown   int           `json:"unrecognised_log_lines"`
	Linux        *linuxInfo    `json:"linux"`
	Windows      *windowsInfo  `json:"windows"`
}

type reportSystem struct {
	OS      string `json:"os"`
	Arch    string `json:"arch"`
	Name    string `json:"name"`    // distribution or "windows"
	Version string `json:"version"` // of the distribution or of Windows
}

type reportState struct {
	Installed *bool `json:"installed"` // null: cannot tell
	// InstalledVersion may differ from the version making the report; "" if unknown
	InstalledVersion string `json:"installed_version"`
	Running          *bool  `json:"running"`
	DNSOverHTTPS     *bool  `json:"dns_over_https"`
	OwnSites         *int   `json:"own_sites"` // how many, never which
	LearnedSites     *int   `json:"learned_sites"`
}

type reportNetwork struct {
	OtherBypassTools []string `json:"other_bypass_tools"`
	IPv6             bool     `json:"ipv6"`             // a public IPv6 address exists
	TunnelInterface  bool     `json:"tunnel_interface"` // something like a VPN is up
}

// A check has a stable id and status (ok, warning, failed, skipped); when it is
// not ok, code says why.
type reportCheck struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Code   string `json:"code"`
}

type tryResult struct {
	Answered int    `json:"answered"`
	Tries    int    `json:"tries"`
	Failure  string `json:"failure"` // reset, timeout, closed, error, tcp-fail
}

type siteReport struct {
	Host       string     `json:"host"`
	Name       string     `json:"name_lookup"` // ok, failed, private_address, hosts_file
	Route      string     `json:"route"`       // fragma, direct
	AsIs       *tryResult `json:"as_is"`
	Fragmented *tryResult `json:"fragmented"` // only tried when fragma is off
}

type linuxInfo struct {
	Kernel         string `json:"kernel"`
	Nftables       string `json:"nftables"`
	ServiceEnabled *bool  `json:"service_enabled"`
}

type windowsInfo struct {
	SystemProxy    string `json:"system_proxy"` // fragma, other, setup_script, none, unknown
	StartsAtSignIn bool   `json:"starts_at_sign_in"`
	AppsEntries    bool   `json:"apps_hosts_entries"`
	RestorePending bool   `json:"restore_pending"`
}

// platformData is what the system-specific part collects.
type platformData struct {
	name, version string
	state         reportState
	checks        []reportCheck
	proxyAddr     string // where to send test connections when fragma works as a proxy
	hostsEntries  bool   // the hosts file points some names at this computer
	logStatus     string
	logLines      []string // raw; only recognised codes leave this program
	linux         *linuxInfo
	windows       *windowsInfo
}

func check(id string, ok bool, code string) reportCheck {
	if ok {
		return reportCheck{ID: id, Status: "ok"}
	}
	return reportCheck{ID: id, Status: "failed", Code: code}
}

func warn(id string, bad bool, code string) reportCheck {
	if !bad {
		return reportCheck{ID: id, Status: "ok"}
	}
	return reportCheck{ID: id, Status: "warning", Code: code}
}

func boolp(v bool) *bool { return &v }

var plainToken = regexp.MustCompile(`^[A-Za-z0-9._-]{1,32}$`)

// token lets a value from the system through only if it looks like a name or
// version number.
func token(s string) string {
	if plainToken.MatchString(s) {
		return s
	}
	return "unknown"
}

var saidVersion = regexp.MustCompile(`^fragma (\d{1,4}\.\d{1,4}\.\d{1,4})\s*$`)

// versionOf reads the answer of `fragma version`.
func versionOf(answer string) string {
	if m := saidVersion.FindStringSubmatch(answer); m != nil {
		return m[1]
	}
	return ""
}

// sameVersion warns when the installed program is another one than this.
func sameVersion(installed string) reportCheck {
	return warn("version", installed != "" && installed != version, "FR_VERSION_DIFFERS")
}

// at most three parts, so that an address can never pass for a version
var versionNumber = regexp.MustCompile(`\d{1,6}(\.\d{1,6}){1,2}`)

var releaseNumber = regexp.MustCompile(`^\d{1,4}(\.\d{1,4}){0,2}$`)

func numberIn(s string) string {
	if v := versionNumber.FindString(s); v != "" {
		return v
	}
	return "unknown"
}

// countHosts gives only how many names a list holds, never the names; nil if it
// cannot be read.
func countHosts(path string) *int {
	f, err := os.Open(path)
	if path == "" || errors.Is(err, fs.ErrNotExist) {
		return new(int)
	}
	if err != nil {
		return nil
	}
	defer f.Close()
	n := len(parseHosts(f))
	return &n
}

// siteTester tests one site; a variable so that tests need no network.
var siteTester = testSite

// testSite connects to a site a few times. With fragma on it measures what other
// programs get: on Linux every connection already goes through fragma, on Windows
// the test goes through its proxy. With fragma off it asks over DNS-over-HTTPS,
// like probe, and tries both as is and fragmented.
func testSite(ctx context.Context, host string, on bool, data platformData) siteReport {
	r := siteReport{Host: host, Route: "direct"}
	if on {
		r.Route = "fragma"
	}
	system := systemDNS(ctx, host)
	switch {
	case len(system) == 0:
		r.Name = "failed"
	case data.hostsEntries && isAppHost(host) && system[0].IsLoopback():
		r.Name = "hosts_file"
	case bogon(system[0]):
		r.Name = "private_address"
	default:
		r.Name = "ok"
	}
	var shake func(*strategy) string
	switch {
	case on && data.windows != nil && data.proxyAddr == "":
		return r // running, but where its proxy listens is not known
	case on && data.proxyAddr != "":
		shake = func(*strategy) string { return handshakeVia(ctx, data.proxyAddr, host, "443") }
	default:
		ips := system
		if !on {
			ips = dohDNS(ctx, host)
		}
		if len(ips) == 0 {
			return r
		}
		shake = func(s *strategy) string { return handshakeWith(ctx, ips[0], host, s) }
	}
	r.AsIs = repeat(func() string { return shake(nil) })
	if !on {
		for i := range strategies {
			if strategies[i].name == "record-frag" {
				r.Fragmented = repeat(func() string { return shake(&strategies[i]) })
			}
		}
	}
	return r
}

func repeat(try func() string) *tryResult {
	r := &tryResult{Tries: attempts}
	for i := 0; i < attempts; i++ {
		if label := try(); reached(label) {
			r.Answered++
		} else {
			r.Failure = label
		}
		time.Sleep(150 * time.Millisecond)
	}
	return r
}

// handshakeVia does a TLS handshake with host through an HTTP proxy, the way a
// program using the proxy does. Like handshake it does not judge certificates.
func handshakeVia(ctx context.Context, proxyAddr, host, port string) string {
	ctx, cancel := context.WithTimeout(ctx, stepTimeout)
	defer cancel()
	c, err := (&net.Dialer{}).DialContext(ctx, "tcp", proxyAddr)
	if err != nil {
		return "tcp-fail"
	}
	defer c.Close()
	deadline, _ := ctx.Deadline()
	c.SetDeadline(deadline)
	target := net.JoinHostPort(host, port)
	fmt.Fprintf(c, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", target, target)
	resp, err := http.ReadResponse(bufio.NewReader(c), &http.Request{Method: http.MethodConnect})
	if err != nil || resp.StatusCode != http.StatusOK {
		return "tcp-fail"
	}
	t := tls.Client(c, &tls.Config{ServerName: host, InsecureSkipVerify: true, MinVersion: tls.VersionTLS12})
	return classify(t.HandshakeContext(ctx))
}

// systemDNS resolves a name the way other programs on this machine do.
func systemDNS(ctx context.Context, host string) []net.IP {
	ctx, cancel := context.WithTimeout(ctx, stepTimeout)
	defer cancel()
	ips, _ := net.DefaultResolver.LookupIP(ctx, "ip4", host)
	return ips
}

func networkFacts() reportNetwork {
	n := reportNetwork{OtherBypassTools: []string{}}
	n.OtherBypassTools = append(n.OtherBypassTools, bypassRunning()...)
	ifs, _ := net.Interfaces()
	for _, i := range ifs {
		if i.Flags&net.FlagUp == 0 || i.Flags&net.FlagLoopback != 0 {
			continue
		}
		name := strings.ToLower(i.Name)
		for _, p := range []string{"tun", "tap", "wg", "ppp", "wireguard", "tailscale", "zt", "proton", "nordlynx"} {
			if strings.HasPrefix(name, p) {
				n.TunnelInterface = true
			}
		}
		addrs, _ := i.Addrs()
		for _, a := range addrs {
			if ip, ok := a.(*net.IPNet); ok && ip.IP.To4() == nil && ip.IP.IsGlobalUnicast() && !ip.IP.IsPrivate() {
				n.IPv6 = true
			}
		}
	}
	return n
}

// buildReport puts a report together from what was collected.
func buildReport(ctx context.Context, data platformData, network reportNetwork, testSites bool, now time.Time) Report {
	r := Report{
		Schema: reportSchema, Version: version, Date: now.UTC().Format("2006-01-02"),
		System: reportSystem{OS: runtime.GOOS, Arch: runtime.GOARCH, Name: data.name, Version: data.version},
		State:  data.state, Network: network, SitesTested: testSites,
		Checks: append([]reportCheck{}, data.checks...), FailedChecks: []string{}, Sites: []siteReport{},
		LogStatus: data.logStatus, Linux: data.linux, Windows: data.windows,
	}
	r.LogEvents, r.LogUnknown = classifyLog(data.logLines)
	r.Checks = append(r.Checks, warn("other_tools", len(network.OtherBypassTools) > 0, "FR_OTHER_TOOL"))
	if testSites {
		on := data.state.Running != nil && *data.state.Running
		r.Sites = make([]siteReport, len(reportHosts))
		var wg sync.WaitGroup
		for i, h := range reportHosts {
			wg.Add(1)
			go func() { defer wg.Done(); r.Sites[i] = siteTester(ctx, h, on, data) }()
		}
		wg.Wait()
		r.Checks = append(r.Checks, siteChecks(r.Sites)...)
	} else {
		for _, id := range []string{"network", "name_lookup", "sites"} {
			r.Checks = append(r.Checks, reportCheck{ID: id, Status: "skipped"})
		}
	}
	for _, c := range r.Checks {
		if c.Status == "failed" {
			r.FailedChecks = append(r.FailedChecks, c.ID)
		}
	}
	return r
}

func siteChecks(sites []siteReport) []reportCheck {
	answers := func(t *tryResult) bool { return t != nil && t.Answered > 0 }
	network, lookup, private, failing := true, true, false, false
	for _, s := range sites {
		if s.Host == controlHost {
			network = answers(s.AsIs)
			continue
		}
		lookup = lookup && s.Name != "failed"
		private = private || s.Name == "private_address"
		// with fragma off, a site that opens fragmented is not a failure
		failing = failing || !(answers(s.AsIs) && s.AsIs.Answered == s.AsIs.Tries) &&
			!(answers(s.Fragmented) && s.Fragmented.Answered == s.Fragmented.Tries)
	}
	name := check("name_lookup", lookup, "FR_DNS_FAILED")
	if lookup && private {
		name = check("name_lookup", false, "FR_DNS_PRIVATE")
	}
	return []reportCheck{check("network", network, "FR_NO_NETWORK"), name, check("sites", !failing, "FR_SITE_FAILS")}
}

func (r Report) json() []byte {
	b, _ := json.MarshalIndent(r, "", "  ")
	return append(b, '\n')
}

// text is the report for people, in their language. Ids and codes stay as they
// are in the JSON form, so that both can be compared.
func (r Report) text() []byte {
	var b bytes.Buffer
	line := func(key, value string) { fmt.Fprintf(&b, "%s: %s\n", T(key), value) }
	yes := func(v *bool) string {
		switch {
		case v == nil:
			return T("rep.unknown")
		case *v:
			return T("rep.yes")
		}
		return T("rep.no")
	}
	count := func(v *int) string {
		if v == nil {
			return T("rep.unknown")
		}
		return fmt.Sprint(*v)
	}
	fmt.Fprintf(&b, "fragma report (%s %d)\n", T("rep.form"), r.Schema)
	line("rep.version", r.Version)
	line("rep.date", r.Date)
	line("rep.system", strings.TrimSpace(fmt.Sprintf("%s/%s %s %s", r.System.OS, r.System.Arch, r.System.Name, r.System.Version)))
	line("rep.installed", yes(r.State.Installed))
	if v := r.State.InstalledVersion; v != "" {
		line("rep.instver", v)
	}
	line("rep.running", yes(r.State.Running))
	line("rep.doh", yes(r.State.DNSOverHTTPS))
	line("rep.own", count(r.State.OwnSites))
	line("rep.learned", count(r.State.LearnedSites))
	tools := strings.Join(r.Network.OtherBypassTools, ", ")
	if tools == "" {
		tools = T("rep.none")
	}
	line("rep.tools", tools)
	line("rep.ipv6", yes(&r.Network.IPv6))
	line("rep.tunnel", yes(&r.Network.TunnelInterface))
	if l := r.Linux; l != nil {
		line("rep.kernel", l.Kernel)
		line("rep.nft", l.Nftables)
		line("rep.boot", yes(l.ServiceEnabled))
	}
	if w := r.Windows; w != nil {
		line("rep.proxy", w.SystemProxy)
		line("rep.boot", yes(&w.StartsAtSignIn))
		line("rep.hosts", yes(&w.AppsEntries))
		line("rep.pending", yes(&w.RestorePending))
	}
	fmt.Fprintf(&b, "\n%s\n", T("rep.checks"))
	for _, c := range r.Checks {
		fmt.Fprintf(&b, "  %-18s %-8s %s\n", c.ID, c.Status, c.Code)
	}
	fmt.Fprintf(&b, "\n%s\n", T("rep.sites"))
	if !r.SitesTested {
		fmt.Fprintf(&b, "  %s\n", T("rep.nottested"))
	}
	result := func(t *tryResult) string {
		if t == nil {
			return "-"
		}
		s := fmt.Sprintf("%d/%d", t.Answered, t.Tries)
		if t.Failure != "" {
			s += " " + t.Failure
		}
		return s
	}
	for _, s := range r.Sites {
		fmt.Fprintf(&b, "  %-20s %s=%s %s=%s", s.Host, T("rep.lookup"), s.Name, s.Route, result(s.AsIs))
		if s.Fragmented != nil {
			fmt.Fprintf(&b, " %s=%s", T("rep.fragmented"), result(s.Fragmented))
		}
		fmt.Fprintln(&b)
	}
	fmt.Fprintf(&b, "\n%s (%s)\n", T("rep.log"), r.LogStatus)
	for _, e := range r.LogEvents {
		fmt.Fprintf(&b, "  %-26s x%d\n", e.Code, e.Count)
	}
	if r.LogUnknown > 0 {
		fmt.Fprintf(&b, "  %s: %d\n", T("rep.unrecognised"), r.LogUnknown)
	}
	return b.Bytes()
}

// writeNewFile writes data to a file that does not exist yet, readable by its
// owner only.
func writeNewFile(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// report shows the report on out and, if asked, saves exactly what was shown.
func report(ctx context.Context, args []string, out, info io.Writer) error {
	fl := flag.NewFlagSet("report", flag.ContinueOnError)
	fl.SetOutput(info)
	asJSON := fl.Bool("json", false, "show the report as JSON")
	output := fl.String("output", "", "also save it to this new file (JSON if the name ends in .json)")
	offline := fl.Bool("no-net", false, "do not test any site")
	if err := fl.Parse(args); err != nil {
		return err
	}
	if fl.NArg() > 0 {
		return errors.New(T("rep.usage"))
	}
	if *output != "" {
		if _, err := os.Lstat(*output); !errors.Is(err, fs.ErrNotExist) {
			return errors.New(T("rep.exists", *output))
		}
	}
	if !*offline {
		fmt.Fprintln(info, "  "+T("rep.testing"))
	}
	r := buildReport(ctx, collectPlatform(), networkFacts(), !*offline, time.Now())
	data := r.text()
	if *asJSON || strings.EqualFold(ext(*output), ".json") {
		data = r.json()
	}
	if _, err := out.Write(data); err != nil {
		return err
	}
	// on info, so that `fragma report > file` holds the report alone
	if *output != "" {
		if err := writeNewFile(*output, data); err != nil {
			return err
		}
		fmt.Fprintf(info, "\n  %s\n", T("rep.saved", *output))
	}
	fmt.Fprintf(info, "\n  %s\n  %s\n", T("rep.nothing"), reportURL)
	return nil
}

func ext(path string) string {
	if i := strings.LastIndexByte(path, '.'); i >= 0 {
		return path[i:]
	}
	return ""
}

func runReport(ctx context.Context, args []string) {
	if err := report(ctx, args, os.Stdout, os.Stderr); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		fail(err.Error())
	}
}
