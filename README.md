# fragma

**Open the sites your ISP blocks (Discord, Roblox, ...) with one command.**
No VPN, no account, no config files. A [zapret](https://github.com/bol-van/zapret) / GoodbyeDPI
alternative for Linux, written in Go.

```bash
curl -fsSL https://raw.githubusercontent.com/vam0laa/fragma/main/install.sh | sudo sh
```

That downloads the latest release, verifies its checksum, installs it and starts it. It also
starts automatically at every boot. To turn it off:

```bash
sudo fragma stop        # off, nothing is redirected anymore
sudo fragma start       # on again
fragma status           # is it on?
sudo fragma uninstall   # remove everything
```

## Does it work?

Measured on a real Turkish ISP connection that blocks Discord and Roblox by SNI (not guessed).
HTTPS requests that got an answer, out of 15:

| Address | Direct | Through fragma |
|---|---|---|
| discord.com | 8 | **15** |
| www.roblox.com | 5 | **15** |
| apis.roblox.com | 3 | **15** |
| auth.roblox.com | 8 | **15** |

The block is intermittent, which is why a single try tells you nothing. It also showed that the
usual trick of splitting the packet at the TCP level does **not** work against this ISP, while
splitting the TLS *record* does (30/30 vs about half). Your ISP may differ: run `fragma probe` and
`fragma experiment` to see what yours does.

## How it works

SNI-based DPI reads the host name from the first TLS packet and cuts the connection. fragma
sits between your apps and the network and rewrites that first packet as two valid TLS records,
cut inside the host name. The server reassembles them, the DPI box does not.

System-wide mode (what `install` sets up) does two things for every app on the machine:

1. **HTTPS:** an nftables rule redirects outgoing TCP port 443 to fragma, which fragments the
   ClientHello and forwards the connection to its real destination.
2. **DNS:** plain DNS (UDP 53) is answered over DNS-over-HTTPS (Cloudflare, with Quad9 and Google
   as fallback), so poisoned or blocked DNS answers do not matter.

Local and LAN addresses are never touched. fragma's own connections are marked so they are not
redirected back into itself.

## What it changes on your system

Nothing else than this, and `sudo fragma uninstall` removes all of it:

- `/usr/local/bin/fragma`
- `/etc/systemd/system/fragma.service`
- an nftables table `ip fragma`, which exists only while the service runs (stopping the service,
  even a crash, removes it)

**Privacy note:** with DNS enabled, the names you look up go to the DoH provider instead of your
ISP. Start with `--no-dns` or your own server (`--doh https://...`) if you prefer. `fragma run`
takes these flags; edit `ExecStart` in the service file to use them.

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

## Limitations (please read)

- **HTTPS (TCP) only.** UDP traffic is not touched: Discord voice/video calls, and probably
  in-game traffic of Roblox, which use UDP. Roblox's website, login and API (all HTTPS) are covered.
  We have not measured whether your ISP blocks the UDP part.
- IPv4 only for now. It cannot help against blocks by IP address, only against SNI/DNS blocking.
- If a site stops working after installing, `sudo fragma stop` and check again; please open an issue.
- Another tool that rewrites the same traffic (for example zapret) is redundant next to fragma.
- It only changes how *your own* connection is written to the network. You are responsible for
  using it in line with the laws and terms of service that apply to you.

## Other commands

| Command | What it does |
|---|---|
| `fragma probe` | For each site: is DNS poisoned? IP blocked? SNI blocked? Every test is repeated 3 times because DPI is often intermittent. |
| `fragma experiment [hosts...]` | Tries 6 ways of sending the TLS ClientHello, 10 times each, and prints success counts. |
| `fragma proxy [--listen 127.0.0.1:8899] [--only discord.com]` | A local HTTPS proxy for a single app, no root needed (for example `discord --proxy-server=http://127.0.0.1:8899`). [`docs/proxy.pac`](docs/proxy.pac) sends only blocked sites through it. |
| `sudo fragma run` | System-wide mode in the foreground (the service runs this). |

## Development

```bash
make test       # unit tests
make e2e        # end-to-end test of the system-wide mode in a private network namespace
make release    # static linux amd64/arm64 binaries + SHA256SUMS in dist/
```

`make e2e` needs no root and never touches your real network: it builds a fake blocked site and a
lying DNS server inside `unshare -Urn` and checks that the real binary fragments HTTPS, answers DNS
over DoH, leaves LAN addresses alone and removes its rules on stop.

## Roadmap

- [ ] UDP / QUIC support (voice, games)
- [ ] IPv6
- [ ] Automatic technique selection per ISP
- [ ] English / Türkçe output, boxed UI like [vamola-doctor](https://github.com/vam0laa/vamola-doctor)
- [ ] AUR package

## License

[MIT](LICENSE) © 2026 vam0laa
