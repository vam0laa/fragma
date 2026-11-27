# fragma

**Open the sites your ISP blocks (Discord, Roblox, ...) with one command, and leave everything
else exactly as it is.**

[![test](https://github.com/vam0laa/fragma/actions/workflows/test.yml/badge.svg)](https://github.com/vam0laa/fragma/actions/workflows/test.yml)
[![release](https://img.shields.io/github/v/release/vam0laa/fragma)](https://github.com/vam0laa/fragma/releases/latest)
[![license: MIT](https://img.shields.io/badge/license-MIT-blue)](LICENSE)

No VPN, no account, no telemetry. A [zapret](https://github.com/bol-van/zapret) / GoodbyeDPI
alternative, written in Go. **Linux** (system-wide) and **Windows** (experimental, via the system proxy).

[Install](#install) · [How it works](#how-it-works) · [Commands](#commands) ·
[Windows](#windows-experimental) · [Privacy](#privacy) · [Report a problem](#something-not-working-or-working) ·
[Limitations](#limitations-please-read) · [Türkçe](#türkçe)

<p align="center">
  <img src="docs/how-it-works.svg" width="860" alt="Without fragma the blocking box reads the site name and cuts the connection. With fragma the first message is split into two TLS records: the box cannot read the name, the server puts it back together.">
</p>

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/vam0laa/fragma/main/install.sh | sudo sh
```

This is for Linux; for Windows see [below](#windows-experimental). The command downloads the latest
release, verifies its checksum, installs it and starts it. It also starts at every boot. There is
nothing to configure. The installer and all commands speak
**English or Türkçe**, chosen from your system language (force one with `FRAGMA_LANG=tr` or
`FRAGMA_LANG=en`). To turn it off:

```bash
sudo fragma stop        # off, nothing is redirected anymore
sudo fragma start       # on again
fragma status           # is it on?
sudo fragma uninstall   # remove everything
```

### Updating

Run the install command again. The new program is tried first; if the new service does not set its
rules and stay up, **the version you had comes back** with its on/off state. After a good update
the earlier program is kept as `/usr/local/bin/fragma.old`.

| To | Run |
|---|---|
| ask for root only for the install step | `curl -fsSL …/install.sh \| sh` (downloads and checks as you, then uses `sudo`) |
| install a given version | `curl -fsSL …/install.sh \| sudo FRAGMA_VERSION=v0.5.0 sh` |
| install only if the file's origin is confirmed | `curl -fsSL …/install.sh \| FRAGMA_REQUIRE_PROVENANCE=1 sh` |

**What gets verified.** The SHA-256 checksum is always checked; it shows the file arrived whole.
From 0.5.0 on, releases also carry GitHub *provenance*, which shows that this repository's release
workflow built the file. The installer can check it only with the [GitHub CLI](https://cli.github.com)
installed and logged in for the user who runs the script (with `sudo sh`, that is root). Otherwise,
and when the proof cannot be fetched (some networks block the server GitHub keeps it on), it says
**provenance NOT verified** and why, and goes on, unless you required it.

## What you see

<p align="center">
  <img src="docs/terminal.svg" width="680" alt="Terminal: fragma status says fragma is ON; fragma list shows the built-in Discord and Roblox domains, your own sites and the learned ones.">
</p>

## Why fragma

- **Plug and run.** One command; Discord and Roblox are known out of the box, other blocked sites
  are found by themselves.
- **Surgical.** Only connections to blocked sites are changed. Everything else passes byte for byte.
- **Your traffic stays yours.** It is not sent through anyone's server, and DNS is left alone unless
  you ask.
- **Small enough to read.** Go, standard library only, with unit and end-to-end tests.

## Türkçe

<details>
<summary><b>Kısa Türkçe anlatım</b></summary>

<br>

**fragma, internet sağlayıcının engellediği siteleri (Discord, Roblox ...) tek komutla açar; diğer
her şeye dokunmaz.** VPN yok, hesap yok, telemetri yok.

**Nasıl çalışır?** Sağlayıcıdaki engel kutusu, bağlantının ilk mesajındaki site adını okuyup
bağlantıyı keser. fragma bu mesajı site adının ortasından iki geçerli parçaya böler: sunucu
parçaları birleştirir, engel kutusu adı göremez.

**Kurulum (Linux)**

```bash
curl -fsSL https://raw.githubusercontent.com/vam0laa/fragma/main/install.sh | sudo sh
```

| Komut | Ne yapar |
|---|---|
| `fragma status` | açık mı? |
| `sudo fragma stop` / `start` | kapat / tekrar aç |
| `sudo fragma add <site>` | bu siteyi de engelli say |
| `fragma report` | sorun mu var? paylaşabileceğin raporu gösterir |
| `sudo fragma uninstall` | her şeyi kaldır |

**Güncelleme:** Kurulum komutunu yeniden çalıştır. Yeni sürüm çalışmazsa eskisi kendiliğinden geri
gelir. Sağlama toplamı her zaman denetlenir; dosyayı projenin ürettiğinin doğrulanması için GitHub
CLI (`gh`) kurulu ve giriş yapılmış olmalıdır, yoksa betik "kaynak DOĞRULANMADI" diye bildirir.

**Sorun bildirme:** `fragma report` hiçbir şey göndermez. Raporu oku, istersen
[buraya yapıştır](https://github.com/vam0laa/fragma/issues/new?template=report.yml). Raporda IP
adresi, kullanıcı adı, dosya yolu veya site listen yer almaz.

**Gizlilik:** Telemetri yok. Sistem geneli mod DNS'ine dokunmaz; `fragma proxy`, `probe`,
`experiment`, `report` ve Windows'taki `apps` ise bazı adları Cloudflare'e (`probe` ayrıca
Google'a) sorar. Ayrıntı için yukarıdaki **Privacy** bölümüne bak.

**Bilmen gerekenler:** Yalnızca HTTPS (TCP) ve IPv4; Discord sesli görüşme gibi UDP trafiğine
dokunmaz. Site adına bakan engellere karşı çalışır, IP engellerine karşı çalışmaz. Windows sürümü (arka planda,
sistem proxy'si üzerinden çalışır) deneyseldir. Aracı bulunduğun yerin yasalarına uygun
kullanmak senin sorumluluğundadır.

</details>

## It only touches blocked sites

fragma changes only the connections to blocked sites, and needs no setup to do so.
(Zapret can do the same with its host lists; here it is the default.)

- A site that is **not blocked is passed through byte for byte**, unchanged.
- Sites that **are** blocked get their first TLS packet rewritten so the blocking box cannot read
  the site name. fragma knows Discord and Roblox out of the box.
- Any other blocked site is found **automatically**: if a normal connection is cut (the typical
  sign of a block) and the same connection works fragmented, fragma remembers that site.
- You can add sites yourself: `sudo fragma add example.com`. `fragma list` shows what fragma changes.

Some servers even refuse a fragmented hello. fragma notices that too, goes back to the plain
connection and remembers it, so sites that were never blocked keep working.

## Privacy

fragma is built so that nothing about you has to leave your machine:

- **No telemetry, analytics, accounts or update checks.** It never contacts a server of ours (there
  is none).
- **No logs of the sites you visit.** The Linux service prints only "on", "off" and errors; the
  Windows error log holds errors only.
- **Your traffic goes exactly where it was going.** fragma changes how the first packet is cut, not
  where it is sent.
- **DNS is not touched by default** by the system-wide mode (Linux) and the background mode
  (Windows). Your lookups keep going wherever they went before. If your ISP also poisons DNS, you
  can opt in with `sudo fragma dns on` (answers over DNS-over-HTTPS, Cloudflare by default, or your own: `sudo fragma dns on --doh https://your.server/dns-query`).
  Then your lookups go to that provider instead of your ISP. A test checks that with DNS off not a
  single request goes to a DoH server.
- The learned list stays on your machine (`/var/lib/fragma/learned.txt`).
- **Some parts do use other resolvers.** Know them:

  | What | Where names are looked up |
  |---|---|
  | `fragma proxy` | Cloudflare over HTTPS (`1.1.1.1`), the system resolver if that gives nothing |
  | Windows `fragma apps on` | Cloudflare over HTTPS for the Discord and Roblox names in the hosts file |
  | `--dns` / `fragma dns on` | Cloudflare, or the server you give with `--doh` (Linux) |
  | `fragma probe` | Google (`8.8.8.8`, unencrypted, to see whether it is tampered with) and Cloudflare over HTTPS |
  | `fragma experiment` | Cloudflare over HTTPS |
  | `fragma report` | your system resolver; with fragma off also Cloudflare over HTTPS |
- The other network use is yours to trigger: the installer downloads from github.com (GitHub sees
  your IP like for any download), `gh` asks GitHub about provenance, and `probe`, `experiment` and
  `report` connect to a few public sites to test them (`report --no-net` does not).
- It is a few thousand lines of Go and uses only the standard library. Read it.

## Does it work?

Measured on a real connection whose ISP blocks Discord and Roblox by SNI (not guessed). HTTPS
requests that got an answer, out of 15:

| Address | Direct | Through fragma |
|---|---|---|
| discord.com | 8 | **15** |
| www.roblox.com | 5 | **15** |
| apis.roblox.com | 3 | **15** |
| auth.roblox.com | 8 | **15** |

The block is intermittent, which is why a single try tells you nothing. The measurements also showed
that the usual trick of splitting the packet at the TCP level does **not** work against this ISP, while
splitting the TLS *record* does (30/30 vs about half). Your ISP may differ: run `fragma probe` and
`fragma experiment` to see what yours does.

## How it works

SNI-based DPI reads the host name from the first TLS packet and cuts the connection. For a blocked
site fragma rewrites that first packet as two valid TLS records, cut inside the host name. The
server reassembles them, the DPI box does not.

System-wide mode (what `install` sets up): an nftables rule sends outgoing HTTPS (TCP 443) through
fragma, which decides per connection, by the site name, whether to change anything, and forwards it to
its real destination. Local and LAN addresses are never touched, and fragma's own connections are
marked so they are not redirected back into itself.

## What it changes on your system

On Linux nothing else than this, and `sudo fragma uninstall` removes the program and the service:

- `/usr/local/bin/fragma` (after an update also `fragma.old`, the version before) and
  `/etc/systemd/system/fragma.service`
- `/etc/fragma/hosts.txt` (your own list, yours to edit) and `/var/lib/fragma/` (learned sites)
- an nftables table `ip fragma`, which exists only while the service runs (stopping the service,
  even a crash, removes it)

## Requirements

Linux with systemd and nftables, x86_64 or arm64. On Arch: `pacman -S nftables`.
If you would rather not pipe a script into a root shell, read
[`install.sh`](install.sh) first, or do it by hand:

```bash
curl -LO https://github.com/vam0laa/fragma/releases/latest/download/fragma-linux-amd64.tar.gz
curl -LO https://github.com/vam0laa/fragma/releases/latest/download/SHA256SUMS
sha256sum --ignore-missing -c SHA256SUMS
gh attestation verify fragma-linux-amd64.tar.gz --repo vam0laa/fragma \
  --signer-workflow vam0laa/fragma/.github/workflows/release.yml   # releases from 0.5.0 on
tar xzf fragma-linux-amd64.tar.gz && sudo ./fragma install
```

## Windows (experimental)

On Windows fragma runs in the background and sets itself as your **proxy**. No administrator
rights and no driver are needed. CI tests it on Windows against real proxy settings and a real
hosts file, but it has **not been tried on many computers or connections**: restarting, signing in,
the administrator prompt and antivirus programs are not covered by CI. On one Windows 11 connection
with SNI blocking and DNS blocking, Discord and Roblox failed directly and answered 5 of 5 requests
with `--dns` (version 0.4.0). Please [tell us how it goes](#something-not-working-or-working).

1. Download `fragma-windows-amd64.zip` from the [latest release](https://github.com/vam0laa/fragma/releases/latest),
   compare it with `SHA256SUMS` (`certutil -hashfile fragma-windows-amd64.zip SHA256`) and unzip it.
   The program is not code-signed, so Windows SmartScreen or your antivirus may warn; you can also
   build it yourself with `go build`.
2. Open PowerShell in that folder and run `.\fragma.exe install`. It starts now and at every sign-in.
   To try it without installing: `.\fragma.exe start`, later `.\fragma.exe stop`.
3. If blocked sites still do not open, your ISP probably poisons DNS too (`fragma probe` shows it). Use
   `.\fragma.exe install --dns`; your name lookups then go to Cloudflare.

| Command | What it does |
|---|---|
| `fragma install` / `uninstall` | Copy to `%LocalAppData%\Programs\fragma` and start at sign-in / remove it. |
| `fragma start` / `stop` / `restart` / `status` | Turn it on or off. |
| `fragma run` | Like `start`, but in the current window (Ctrl+C stops). |
| `fragma add <site>`, `fragma list` | Same as on Linux. Files are in `%AppData%\fragma`. |
| `fragma apps on` / `off` / `status` | The hosts file entries for apps that ignore the proxy (see below). |
| `fragma report` | See [below](#something-not-working-or-working). |
| `--dns` (for `install`, `start`, `run`) | Resolve names over DNS-over-HTTPS (Cloudflare). Off by default. |

**What it changes:** the current user's proxy settings (`127.0.0.1:8899`; plain HTTP is passed on unchanged; local and LAN
addresses bypassed) while it runs, a `fragma` entry under the user's `Run` registry key after
`install`, and a temporary `RunOnce` entry. Your previous proxy settings are saved first and put back
by `stop`; after a crash by a small guard process; after a reboot or power loss at the next sign-in.
What you change in the proxy settings while fragma runs is kept. The saved settings are written with
a second copy; should both be unreadable, fragma turns its own proxy off and tells you. If
restoring fails, a message box and `error.log` in `%LocalAppData%\fragma` say so, and it is tried
again at the next sign-in. If another proxy or setup script is already configured, fragma refuses to
start.

**Updating:** run `.\fragma.exe install` from the new download. The new program is tried first; if
it cannot be started, the earlier one and its sign-in entry come back. The version before is kept
next to it as `fragma.exe.old`.

**Apps that ignore the proxy** (the Discord desktop app's updater does): run `fragma apps on` once and
approve the administrator prompt. It adds a marked block to the Windows hosts file that points
Discord and Roblox names at your own machine, where fragma relays them and looks up the real address
over DNS-over-HTTPS (Cloudflare), also without `--dns`. **Those names work only while fragma runs**;
`stop` and `status` remind you. Remove the block with `fragma apps off` (`uninstall` does it too).
Before each change the file is copied to `hosts.fragma-backup` next to it; the new file replaces it
in one step and keeps its owner and permissions.

**Limits:** only programs that follow the Windows proxy settings go through fragma (browsers do;
some apps and games do not; see `fragma apps` above for Discord). VPN and dial-up connections have their own proxy settings,
which are not changed. `fragma proxy` still starts only the local proxy, without touching Windows.
Where you need packet-level handling, use GoodbyeDPI.

## Something not working? (or working!)

fragma has no telemetry, so we only know what you tell us. Run

```bash
fragma report                        # shows the report
fragma report --output report.json   # also saves exactly what it showed (JSON for .json)
fragma report --no-net               # without testing any site
```

read it, and if you are happy to share it, paste it into a
[report issue](https://github.com/vam0laa/fragma/issues/new?template=report.yml). "It works", with
the name of your provider, helps as much as a problem does.

- **In it:** the version (also of the installed program), the system, whether fragma runs and starts
  by itself, checks with codes such as `FR_NOT_RUNNING`, test results for five fixed public sites,
  **how many** sites are on your lists, and which known messages the log holds, with their number.
- **Not in it:** your sites, IP addresses, user or computer names, file paths or any line of your
  log. The report is built from fixed words and numbers; text from your computer is not copied.
- Nothing is sent. Issues are public.
- On Linux the service log is readable with `sudo` (or in the `systemd-journal` group); without it
  the report says so.

## fragma and Zapret

Both get around SNI-based blocks, in different ways. Neither is "better": they suit different cases.

| | Zapret | fragma |
|---|---|---|
| Approach | Packet level (NFQUEUE): fake packets, TTL tricks, splitting, reordering. Also a proxy mode (`tpws`) | Transparent proxy: rewrites the first TLS message into two valid TLS records |
| Techniques | Many; you pick what works on your ISP (`blockcheck` helps) | One: TLS record fragmentation |
| UDP / QUIC / IPv6 / HTTP (port 80) | Yes | No |
| Platforms | Linux, OpenWrt routers, BSD, macOS, Windows | Linux. Windows: experimental, system proxy |
| Setup | Pick a strategy, edit a config | None for the typical case |
| Only blocked sites | Yes, with host lists / auto host list | Yes, by default |
| Overhead | Typically only the first packets of a connection | Every HTTPS (port 443) connection is relayed by fragma |
| Size | C, large feature set | Go, a few thousand lines, standard library only |

**Choose fragma** for a desktop Linux machine, an SNI-based block (Discord, Roblox, ...) and a simple,
no-configuration tool. **Choose Zapret** for UDP/QUIC blocks, routers, BSD/macOS, difficult ISPs, or when you want
to tune the technique yourself.

On the connection we measured, Zapret's installed strategy (fake packets with a short TTL) did not help while TLS
record fragmentation did. That is one ISP and one strategy: another Zapret strategy may well work there too.

## Limitations (please read)

- **HTTPS (TCP) only.** UDP traffic is not touched. That includes Discord voice/video calls and
  probably Roblox in-game traffic. On the ISP we tested, Discord voice works without any help
  because that ISP blocks only the HTTPS side; an ISP that also blocks UDP is not covered yet.
- Automatic detection recognises blocks that **cut the connection** (reset). A block that silently
  drops packets is not detected; add such a site by hand with `sudo fragma add`.
- IPv4 only for now. It cannot help against blocks by IP address, only against SNI blocking (and
  DNS blocking if you turn on `fragma dns`).
- With DNS over HTTPS on, lookups stop while its server cannot be reached; `sudo fragma dns off`
  goes back to your own. A server given by name is looked up when fragma starts; if all its
  addresses change while it runs, restart fragma.
- Do not run another tool that rewrites the same traffic (for example Zapret) at the same time as fragma; use one of them.
- It only changes how *your own* connection is written to the network. You are responsible for
  using it in line with the laws and terms of service that apply to you.

## Commands

| Command | What it does |
|---|---|
| `sudo fragma install` / `stop` / `start` / `restart` / `uninstall`, `fragma status` | Install, update and control the background service. |
| `fragma report [--json] [--output FILE] [--no-net]` | Show what is needed to look into a problem; sends nothing. |
| `fragma version` | The program's version. |
| `sudo fragma add <site>` | Also treat this site as blocked (applies within 10 seconds). |
| `fragma list` | Show the built-in, your own and the learned sites. Every other site is untouched. |
| `sudo fragma dns on [--doh URL]` / `off` | Optional DNS-over-HTTPS, off by default. |
| `fragma probe` | For each site: is DNS poisoned? IP blocked? SNI blocked? Every test is repeated 3 times because DPI is often intermittent. |
| `fragma experiment [hosts...]` | Tries 6 ways of sending the TLS ClientHello, 10 times each, and prints success counts. |
| `fragma proxy [--listen 127.0.0.1:8899] [--add site]` | A local HTTPS proxy for a single app, no root needed (for example `discord --proxy-server=http://127.0.0.1:8899`). [`docs/proxy.pac`](docs/proxy.pac) sends only blocked sites through it. |
| `sudo fragma run [--dns] [--doh URL] [--hosts FILE] [--state DIR]` | System-wide mode in the foreground (the service runs this). |

## Development

```bash
make test       # unit tests
make e2e        # end-to-end test of the system-wide mode in a private network namespace
make dist       # the release files, manifest.json and SHA256SUMS in dist/, checked
scripts/test/update.sh && scripts/test/install.sh   # updating and install.sh, touching nothing
```

`make e2e` needs no root and never touches your real network: it builds a fake blocked site, an
innocent one, a LAN device, a DPI box and a lying DNS server inside `unshare -Urn`, and checks that
the real binary fragments only what it should, learns a blocked site, leaves DNS alone by default
and removes its rules on stop.

CI also installs and updates on a real systemd, and runs the Windows tests against the runner's
proxy settings and hosts file (`scripts/test/systemd.sh` and the integration tests change the
system they run on, so they refuse to run anywhere else). Releases are made by a workflow that stops
at a draft: [docs/releasing.md](docs/releasing.md).

## Roadmap

- [x] Windows background mode through the system proxy
- [ ] Windows packet-level mode for apps that ignore the proxy (needs a driver such as WinDivert)
- [ ] UDP / QUIC support (voice, games)
- [ ] IPv6
- [ ] Detect silent-drop blocks
- [x] English / Türkçe messages
- [ ] AUR package

## License

[MIT](LICENSE) © 2026 vam0laa
