# fragma

**Open the sites your ISP blocks (Discord, Roblox, ...) with one command, and leave everything
else exactly as it is.** No VPN, no account, no telemetry. A [zapret](https://github.com/bol-van/zapret) /
GoodbyeDPI alternative, written in Go. **Linux** (system-wide) and **Windows** (experimental, local proxy).

```bash
curl -fsSL https://raw.githubusercontent.com/vam0laa/fragma/main/install.sh | sudo sh
```

That downloads the latest release, verifies its checksum, installs it and starts it. It also
starts at every boot. The installer and all commands speak **English or Türkçe**, chosen from your
system language (force one with `FRAGMA_LANG=tr` or `FRAGMA_LANG=en`). To turn it off:

```bash
sudo fragma stop        # off, nothing is redirected anymore
sudo fragma start       # on again
fragma status           # is it on?
sudo fragma uninstall   # remove everything
```

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
- **No logs of the sites you visit.** The service prints only "on" and "off".
- **Your traffic goes exactly where it was going.** fragma changes how the first packet is cut, not
  where it is sent.
- **DNS is not touched by default.** Your lookups keep going wherever they went before. If your ISP
  also poisons DNS, you can opt in with `sudo fragma dns on` (answers over DNS-over-HTTPS,
  Cloudflare by default, or your own: `sudo fragma dns on --doh https://your.server/dns-query`).
  Then your lookups go to that provider instead of your ISP. A test checks that with DNS off not a
  single request goes to a DoH server.
- The learned list stays on your machine (`/var/lib/fragma/learned.txt`).
- The only other network use is yours to trigger: the installer downloads from github.com (GitHub
  sees your IP like for any download, the checksum is verified), and `fragma probe` /
  `fragma experiment` contact a few public hosts and one DoH resolver to run their tests.
- It is under 2,000 lines of Go and uses only the standard library. Read it.

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

Nothing else than this, and `sudo fragma uninstall` removes the program and the service:

- `/usr/local/bin/fragma` and `/etc/systemd/system/fragma.service`
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
tar xzf fragma-linux-amd64.tar.gz && sudo ./fragma install
```

## Windows (experimental)

The full automatic mode is Linux only. On Windows the local proxy works: it changes only blocked
sites, learns new ones and remembers them, like on Linux. The code is tested on a real Windows
machine by CI on every change, but it has **not been tried against blocked sites on Windows yet**.
Please tell us how it goes (open an issue).

1. Download `fragma-windows-amd64.zip` from the [latest release](https://github.com/vam0laa/fragma/releases/latest),
   compare it with `SHA256SUMS` (`certutil -hashfile fragma-windows-amd64.zip SHA256`) and unzip it.
   The program is not code-signed, so Windows SmartScreen or your antivirus may warn; you can also
   build it yourself with `go build`.
2. Open a terminal in that folder, run `fragma.exe proxy` and leave it open.
3. Tell Windows to use it: Settings → Network & internet → Proxy → Manual proxy setup → **On**,
   address `127.0.0.1`, port `8899`. Browsers and apps that follow the Windows proxy now go through
   fragma. Discord can also be started with `--proxy-server=http://127.0.0.1:8899`.
4. To undo: switch the manual proxy off and close fragma.

Your own sites and the learned ones live in `%AppData%\fragma` (`fragma add example.com`,
`fragma list`). Where you need a mature, fully automatic Windows tool, use GoodbyeDPI.

## fragma and Zapret

Both get around SNI-based blocks, in different ways. Neither is "better": they suit different cases.

| | Zapret | fragma |
|---|---|---|
| Approach | Packet level (NFQUEUE): fake packets, TTL tricks, splitting, reordering. Also a proxy mode (`tpws`) | Transparent proxy: rewrites the first TLS message into two valid TLS records |
| Techniques | Many; you pick what works on your ISP (`blockcheck` helps) | One: TLS record fragmentation |
| UDP / QUIC / IPv6 / HTTP (port 80) | Yes | No |
| Platforms | Linux, OpenWrt routers, BSD, macOS, Windows | Linux. Windows: experimental proxy |
| Setup | Pick a strategy, edit a config | None for the typical case |
| Only blocked sites | Yes, with host lists / auto host list | Yes, by default |
| Overhead | Typically only the first packets of a connection | Every HTTPS (port 443) connection is relayed by fragma |
| Size | C, large feature set | Go, under 2,000 lines, standard library only |

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
- Do not run another tool that rewrites the same traffic (for example Zapret) at the same time as fragma; use one of them.
- It only changes how *your own* connection is written to the network. You are responsible for
  using it in line with the laws and terms of service that apply to you.

## Commands

| Command | What it does |
|---|---|
| `sudo fragma install` / `stop` / `start` / `uninstall`, `fragma status` | Install and control the background service. |
| `sudo fragma add <site>` | Also treat this site as blocked (applies within 10 seconds). |
| `fragma list` | Show the built-in, your own and the learned sites. Every other site is untouched. |
| `sudo fragma dns on [--doh URL]` / `off` | Optional DNS-over-HTTPS, off by default. |
| `fragma probe` | For each site: is DNS poisoned? IP blocked? SNI blocked? Every test is repeated 3 times because DPI is often intermittent. |
| `fragma experiment [hosts...]` | Tries 6 ways of sending the TLS ClientHello, 10 times each, and prints success counts. |
| `fragma proxy [--listen 127.0.0.1:8899] [--add site]` | A local HTTPS proxy for a single app, no root needed (the way to use fragma on Windows) (for example `discord --proxy-server=http://127.0.0.1:8899`). [`docs/proxy.pac`](docs/proxy.pac) sends only blocked sites through it. |
| `sudo fragma run [--dns] [--doh URL] [--hosts FILE] [--state DIR]` | System-wide mode in the foreground (the service runs this). |

## Development

```bash
make test       # unit tests
make e2e        # end-to-end test of the system-wide mode in a private network namespace
make release    # static linux amd64/arm64 binaries + SHA256SUMS in dist/
```

`make e2e` needs no root and never touches your real network: it builds a fake blocked site, an
innocent one, a LAN device, a DPI box and a lying DNS server inside `unshare -Urn`, and checks that
the real binary fragments only what it should, learns a blocked site, leaves DNS alone by default
and removes its rules on stop.

## Roadmap

- [ ] Windows system-wide mode (needs a packet driver such as WinDivert)
- [ ] UDP / QUIC support (voice, games)
- [ ] IPv6
- [ ] Detect silent-drop blocks
- [x] English / Türkçe messages
- [ ] AUR package

## License

[MIT](LICENSE) © 2026 vam0laa
