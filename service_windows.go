package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// The Windows system mode needs no administrator rights and no driver: it runs
// the local proxy in the background and sets it as the current user's HTTPS
// proxy. Whatever way it ends, the old proxy settings are put back:
//   - stop / Ctrl+C: by the process itself
//   - crash or kill: by a small guard process that waits for it
//   - reboot or power loss: by a RunOnce entry at the next sign-in

const (
	runKey      = `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`
	runOnceKey  = `HKCU\Software\Microsoft\Windows\CurrentVersion\RunOnce`
	defaultPort = 8899

	createNoWindow = 0x08000000
)

// localDir holds the program, the saved proxy settings and the error log.
func localDir(elem ...string) string {
	d := os.Getenv("LOCALAPPDATA")
	if d == "" {
		fail(T("fail.nohome"))
	}
	return filepath.Join(append([]string{d}, elem...)...)
}

func stateFile() string   { return localDir("fragma", "proxy.json") }
func stopFile() string    { return localDir("fragma", "stop") }
func logFile() string     { return localDir("fragma", "error.log") }
func installPath() string { return localDir("Programs", "fragma", "fragma.exe") }

func reg(args ...string) error {
	if out, err := exec.Command("reg", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("reg %s: %v: %s", args[0], err, strings.TrimSpace(string(out)))
	}
	return nil
}

func regSet(key, name, value string) error {
	return reg("add", key, "/v", name, "/t", "REG_SZ", "/d", value, "/f")
}

// hidden prepares a copy of this program that runs without a window.
func hidden(exe string, args ...string) *exec.Cmd {
	c := exec.Command(exe, args...)
	c.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNoWindow | syscall.CREATE_NEW_PROCESS_GROUP}
	return c
}

func self() string {
	exe, err := os.Executable()
	if err != nil {
		fail(err.Error())
	}
	return exe
}

type runOpts struct {
	port int
	dns  bool
}

func parseRunOpts(cmd string, args []string) runOpts {
	var o runOpts
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	fs.IntVar(&o.port, "port", defaultPort, "local proxy port")
	fs.BoolVar(&o.dns, "dns", false, "resolve names over DNS-over-HTTPS (Cloudflare); off by default")
	fs.Parse(args)
	return o
}

func (o runOpts) args() []string {
	a := []string{"--port", strconv.Itoa(o.port)}
	if o.dns {
		a = append(a, "--dns")
	}
	return a
}

// runDaemon is the system mode in the foreground.
func runDaemon(args []string) {
	o := parseRunOpts("run", args)
	release, ok, err := lockInstance()
	for i := 0; err == nil && !ok && i < 20; i++ { // a `restore` may hold it for a moment
		time.Sleep(100 * time.Millisecond)
		release, ok, err = lockInstance()
	}
	if err != nil {
		fail(err.Error())
	}
	if !ok {
		fail(T("win.already"))
	}
	defer release()
	os.Remove(stopFile())

	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(o.port))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		// leftover settings would point at whoever owns the port now
		if rerr := restoreProxy(winProxy{}, stateFile()); rerr != nil {
			fmt.Fprintln(os.Stderr, "  "+T("win.restorefail", rerr))
		}
		fail(err.Error())
	}
	defer ln.Close()

	hosts := newHostlist(userFile("hosts.txt"), userFile("learned.txt"))
	stop := make(chan struct{})
	defer close(stop)
	go hosts.watch(stop)
	// the relay for programs that ignore the proxy (see `fragma apps`)
	relay := &proxy{tlsPort: "443", resolve: cachedResolver(dohDNS), hosts: hosts}
	apps := &appRelay{
		addrs:  []string{"127.0.0.1:443", "[::1]:443"},
		listen: net.Listen,
		serve:  relay.serveDirect,
		report: func(err error) { fmt.Fprintln(os.Stderr, "app relay:", err) },
	}
	apps.update(appsEnabled())
	defer apps.update(false)

	// DNS is left to the system unless asked for: an empty answer makes the
	// proxy use the system resolver.
	// App hosts are the exception while apps are on: the hosts file points them
	// at ourselves.
	resolve := func(ctx context.Context, host string) []net.IP {
		if apps.active() && isAppHost(host) {
			return dohDNS(ctx, host)
		}
		return nil
	}
	mode := T("win.dnsoff")
	if o.dns {
		resolve, mode = dohDNS, T("win.dnson")
	}
	p := &proxy{tlsPort: "443", cache: map[string]cached{}, resolve: resolve, hosts: hosts}
	done := make(chan struct{})
	go func() { p.serve(ln); close(done) }()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)

	// The settings change last, once the proxy is listening.
	if err := regSet(runOnceKey, "fragma-restore", `"`+self()+`" restore`); err != nil {
		fail(err.Error())
	}
	if err := enableProxy(winProxy{}, stateFile(), addr, os.Getpid()); err != nil {
		reg("delete", runOnceKey, "/v", "fragma-restore", "/f")
		if err == errForeignProxy {
			fail(T("win.foreign"))
		}
		fail(err.Error())
	}
	guard := hidden(self(), "guard", strconv.Itoa(os.Getpid()))
	if err := guard.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "guard:", err)
	}
	fmt.Println("  " + T("win.running", addr, mode))

	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
wait:
	for ticks := 1; ; ticks++ {
		select {
		case <-sig:
			break wait
		case <-done:
			break wait
		case <-tick.C:
			if exists(stopFile()) {
				break wait
			}
			if ticks%10 == 0 { // `fragma apps on` or `off` while we run
				apps.update(appsEnabled())
			}
		}
	}
	if err := cleanup(); err != nil {
		fail(T("win.restorefail", err))
	}
	os.Remove(stopFile())
	fmt.Println("  " + T("win.stopped"))
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

// cleanup puts the old proxy settings back and drops the sign-in recovery entry.
func cleanup() error {
	if err := restoreProxy(winProxy{}, stateFile()); err != nil {
		return err
	}
	reg("delete", runOnceKey, "/v", "fragma-restore", "/f") // fails if absent
	return nil
}

// runRestore restores settings left behind by a run that is gone. unattended
// means nobody sees a console: the guard, or the entry run at sign-in. Then
// problems go to the log file and a message box. warnApps adds the warning about
// hosts file entries that now lead nowhere.
func runRestore(unattended, warnApps bool) {
	release, ok, err := lockInstance()
	if err != nil || !ok {
		return // running: the settings are in use
	}
	defer release()
	var notes []string
	if unattended {
		proxyWarn = func(key string, args ...any) { notes = append(notes, T(key, args...)) }
	}
	err = cleanup()
	if err != nil {
		if !unattended {
			fail(T("win.restorefail", err))
		}
		// Windows removes a RunOnce entry before running it: set it again
		if rerr := regSet(runOnceKey, "fragma-restore", `"`+self()+`" restore`); rerr != nil {
			notes = append(notes, rerr.Error())
		}
		notes = append(notes, T("win.restorefail", err))
	}
	// after a crash nothing else tells that Discord and Roblox now lead nowhere
	if warnApps && appsEnabled() {
		notes = append(notes, T("win.appsleft"))
	}
	if len(notes) > 0 {
		tellUser(strings.Join(notes, "\n\n"))
	}
	if err != nil {
		os.Exit(1)
	}
}

// tellUser records a message and shows it, for processes without a console.
func tellUser(text string) {
	os.MkdirAll(filepath.Dir(logFile()), 0o755)
	if f, err := os.OpenFile(logFile(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
		fmt.Fprintf(f, "%s %s\n", time.Now().Format(time.RFC3339), text)
		f.Close()
	}
	if os.Getenv("FRAGMA_NO_DIALOG") == "" { // automated tests have nobody to close it
		messageBox("fragma", text)
	}
}

// runGuard waits for the process pid to end and then restores its settings.
func runGuard(args []string) {
	if len(args) != 1 {
		return
	}
	pid, err := strconv.Atoi(args[0])
	if err != nil {
		return
	}
	if p, err := os.FindProcess(pid); err == nil {
		p.Wait() // an error means we cannot wait for it; the check below still holds
	}
	if s, ok, err := readSaved(stateFile()); err != nil || ok && s.PID == pid {
		runRestore(true, true)
	}
}

// startBackground launches `exe run` hidden and waits until it is working.
func startBackground(exe string, o runOpts) {
	if instanceRunning() {
		stepOK(T("step.running"), T("d.running"))
		return
	}
	os.MkdirAll(filepath.Dir(logFile()), 0o755)
	log, err := os.Create(logFile())
	if err != nil {
		fail(err.Error())
	}
	defer log.Close()
	c := hidden(exe, append([]string{"run"}, o.args()...)...)
	c.Stderr = log
	if err := c.Start(); err != nil {
		fail(err.Error())
	}
	exited := make(chan struct{})
	go func() { c.Wait(); close(exited) }()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
		select {
		case <-exited:
			b, _ := os.ReadFile(logFile())
			fail(T("win.startfail", strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(b)), "✖"))))
		case <-time.After(100 * time.Millisecond):
		}
		if s, ok, _ := readSaved(stateFile()); ok && s.PID == c.Process.Pid {
			if cur, err := (winProxy{}).get(); err == nil && cur.Server == s.Ours {
				stepOK(T("step.running"), T("d.running"))
				return
			}
		}
	}
	c.Process.Kill()
	runRestore(false, false)
	fail(T("win.startfail", logFile()))
}

// stopBackground ends a running system mode and restores the proxy settings.
func stopBackground() {
	if instanceRunning() {
		s, ok, _ := readSaved(stateFile())
		os.MkdirAll(filepath.Dir(stopFile()), 0o755)
		if err := os.WriteFile(stopFile(), nil, 0o600); err != nil {
			fail(err.Error())
		}
		for i := 0; i < 50 && instanceRunning(); i++ {
			time.Sleep(100 * time.Millisecond)
		}
		if instanceRunning() && ok { // stuck: end it, the settings are restored below
			if p, err := os.FindProcess(s.PID); err == nil {
				p.Kill()
				p.Wait()
			}
		}
	}
	os.Remove(stopFile())
	if instanceRunning() {
		fail(T("win.stopfail"))
	}
	runRestore(false, false)
}

func runControl(action string) {
	args := os.Args[2:]
	if action != "start" {
		stopBackground()
	}
	if action == "stop" {
		fmt.Printf("  %s %s\n", paint(cAmber, "○"), T("win.off"))
		warnAppsLeft()
		return
	}
	startBackground(self(), parseRunOpts(action, args))
}

func startsAtSignIn() bool { return reg("query", runKey, "/v", "fragma") == nil }

// warnAppsLeft says what the hosts file entries do while fragma is not running.
func warnAppsLeft() {
	if appsEnabled() {
		fmt.Printf("  %s %s\n", paint(cRed, "!"), T("win.appsleft"))
	}
}

func runStatus() {
	if instanceRunning() {
		fmt.Printf("  %s %s\n", paint(cGreen, "●"), T("st.on", paint(cBold+";"+cGreen, T("word.on")), paint(cDim, T("win.onhint"))))
		return
	}
	fmt.Printf("  %s %s\n", paint(cAmber, "○"), T("st.off", paint(cBold+";"+cAmber, T("word.off")), paint(cDim, T("win.offhint"))))
	if exists(stateFile()) {
		fmt.Println("  " + T("win.leftover"))
	}
	warnAppsLeft()
	os.Exit(3)
}

// runInstall copies the program to %LocalAppData%\Programs\fragma, starts it
// at every sign-in and starts it now.
func runInstall(args []string) {
	o := parseRunOpts("install", args)
	dst, src := installPath(), self()
	stopBackground()
	if !strings.EqualFold(src, dst) {
		b, err := os.ReadFile(src)
		if err == nil {
			err = os.MkdirAll(filepath.Dir(dst), 0o755)
		}
		for i := 0; err == nil; i++ { // the guard may still be closing the old file
			if err = os.WriteFile(dst, b, 0o755); err == nil || i == 20 {
				break
			}
			err = nil
			time.Sleep(100 * time.Millisecond)
		}
		if err != nil {
			fail(T("fail.copy", err))
		}
	}
	stepOK(T("step.installed"), dst)
	auto := `"` + dst + `" start ` + strings.Join(o.args(), " ")
	if err := regSet(runKey, "fragma", auto); err != nil {
		fail(err.Error())
	}
	stepOK(T("step.service"), T("win.autostart"))
	startBackground(dst, o)
	fmt.Println()
	fmt.Println("  " + T("win.manage"))
	fmt.Printf("  & \"%s\" stop | start | status | uninstall\n\n", dst)
}

func runUninstall([]string) {
	stopBackground()
	reg("delete", runKey, "/v", "fragma", "/f") // fails if absent
	if appsEnabled() {
		runApps([]string{"off"}) // the entries are useless without fragma
	}
	dst := installPath()
	if exists(dst) {
		if strings.EqualFold(self(), dst) {
			// a running program cannot delete itself: let cmd do it after we exit
			c := exec.Command("cmd")
			c.SysProcAttr = &syscall.SysProcAttr{
				CreationFlags: createNoWindow,
				CmdLine:       `cmd /d /c ping -n 3 127.0.0.1 >nul & del /f /q "` + dst + `" & rmdir "` + filepath.Dir(dst) + `"`,
			}
			c.Dir = os.TempDir()
			if err := c.Start(); err != nil {
				fail(err.Error())
			}
		} else {
			var err error
			for i := 0; i < 20; i++ {
				if err = os.Remove(dst); err == nil {
					break
				}
				time.Sleep(100 * time.Millisecond)
			}
			if err != nil {
				fail(err.Error())
			}
			os.Remove(filepath.Dir(dst))
		}
	}
	stepOK(T("step.removed"), T("win.removed"))
}

func runAdd(args []string) { runAddUser(args) }
func runList()             { runListUser() }
func runDNS([]string)      { fail(T("win.nodns")) }

// platformCommand handles the commands only this system has.
func platformCommand(name string, args []string) bool {
	switch name {
	case "apps":
		runApps(args)
	case "restore":
		// run at sign-in: no warning if fragma is about to start there as well
		runRestore(true, !startsAtSignIn())
	case "guard":
		runGuard(args)
	default:
		return false
	}
	return true
}
