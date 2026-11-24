# vamola-dpi

Find out **how** your connection is censored, and get around SNI-based blocking with a small
local proxy. No root, no kernel rules, no VPN. *Working title; the name may change.*

It was built from measurements on a real connection, not from guesses:

| Host | Direct | TCP split | **TLS record fragmentation** (this tool) |
|---|---|---|---|
| discord.com | 4/10 | 5/10 | **10/10** |
| gateway.discord.gg | 5/10 | 6-7/10 | **10/10** |
| cdn.discordapp.com | 8/10 | 3-4/10 | **10/10** |

Through the proxy, 60 of 60 real HTTPS requests to Discord succeeded; direct requests succeeded
only 29 of 60 times (even with zapret running). Measured on one ISP, results on yours may differ:
that is what `probe` and `experiment` are for.

## Quick start

```bash
make build
./vamola-dpi probe                      # what is blocked, and how?
./vamola-dpi experiment                 # which trick does your ISP's DPI not understand?
./vamola-dpi proxy                      # start the local proxy on 127.0.0.1:8899
```

Use it:

```bash
curl -x http://127.0.0.1:8899 https://discord.com        # test
discord --proxy-server=http://127.0.0.1:8899             # Discord (quit it fully first)
```

Browsers: set a manual HTTP/HTTPS proxy `127.0.0.1:8899`, or load [`docs/proxy.pac`](docs/proxy.pac)
to send only the blocked sites through the proxy.

Run it in the background as your own user (no root):

```bash
install -Dm755 vamola-dpi ~/.local/bin/vamola-dpi
install -Dm644 packaging/vamola-dpi-proxy.service ~/.config/systemd/user/vamola-dpi-proxy.service
systemctl --user enable --now vamola-dpi-proxy
```

## Commands

| Command | What it does |
|---|---|
| `vamola-dpi probe` | For each site: DNS poisoning? IP block? SNI-based block? Repeats every test 3 times, because DPI is often intermittent. Warns if a bypass tool like zapret is running (it masks the result). |
| `vamola-dpi experiment [hosts...]` | Tries 6 ways of sending the TLS ClientHello, 10 times each, and prints success counts. |
| `vamola-dpi proxy [--listen 127.0.0.1:8899] [--only discord.com,discord.gg]` | HTTP CONNECT proxy. Splits the ClientHello into two TLS records in the middle of the SNI, and resolves names over DNS-over-HTTPS. |

## How it works

SNI-based DPI reads the host name in the first TLS packet and cuts the connection. Many boxes only
look at the first TLS *record*. The proxy re-writes the ClientHello as two valid TLS records, cut
inside the host name. The server reassembles them (the TLS spec allows it); the DPI box does not.
Splitting at the TCP level did **not** help in our measurements, this box reassembles TCP.

## Limitations (please read)

- HTTPS (TCP) only. **Voice/video over UDP (Discord calls) is not covered.**
- Apps must use the proxy. A transparent mode is planned.
- A technique that works on one ISP may not work on another. Run `experiment` first.
- It only changes how *your own* connection is written to the network. You are responsible for
  using it in line with the laws and the terms of service that apply to you.
- It listens on loopback only and refuses any other address, so it cannot become an open proxy.

## Roadmap

- [ ] Transparent mode (nftables redirect) so every app is covered
- [ ] Automatic strategy selection from `experiment`
- [ ] UDP/QUIC desync for voice
- [ ] English / Türkçe output, boxed UI like [vamola-doctor](https://github.com/vam0laa/vamola-doctor)

## License

[MIT](LICENSE) © 2026 vam0laa
