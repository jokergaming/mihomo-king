# mihomo-king

A small Linux TUI for managing [mihomo](https://github.com/MetaCubeX/mihomo)
(Clash.Meta) or [sing-box](https://github.com/SagerNet/sing-box) proxy cores,
built with Go and [Bubble Tea](https://github.com/charmbracelet/bubbletea).

## Features

- Download subscriptions or import them from local files: Clash/mihomo YAML,
  base64 node lists, or node links one per line.
- Add local nodes from share links (vmess, vless, ss, trojan, hysteria2, socks5, http).
- Switch the active subscription and reload the managed core.
- Manage either mihomo or sing-box from the settings screen.
- Convert Clash/mihomo subscriptions into sing-box runtime config.
- Toggle TUN mode.
- Switch the selected node within a proxy group.
- Test node and group latency with a configurable test URL.

## Build & run

```bash
go build -o mihomo-king .
./mihomo-king
```

## Release

GitHub Actions publishes release binaries for Linux and macOS on amd64 and arm64.
Create and push a version tag to publish a release:

```bash
git tag v0.1.0
git push origin v0.1.0
```

The release uploads `tar.gz` archives and SHA-256 checksum files for:

- `linux-amd64`
- `linux-arm64`
- `darwin-amd64`
- `darwin-arm64`

The tool manages the selected core as a background subprocess (start/stop from
the dashboard) and talks to its Clash-compatible API for live changes. It writes
its own settings to `${XDG_CONFIG_HOME:-~/.config}/mihomo-king/`, the active
mihomo config to `~/.config/mihomo/config.yaml`, and the active sing-box config
to `~/.config/sing-box/config.json`.

## TUN requires privileges

Creating a TUN device needs `CAP_NET_ADMIN`/`CAP_NET_RAW`. When you enable TUN in
the dashboard, the tool handles privileges itself: if the selected core binary
lacks the caps it asks for your sudo password (masked, in-TUI) and runs `setcap`
for you, restarting the managed core so the caps take effect. To grant them
ahead of time instead:

```bash
sudo setcap cap_net_admin,cap_net_raw=ep "$(command -v mihomo)"
sudo setcap cap_net_admin,cap_net_raw=ep "$(command -v sing-box)"
```

If another TUN device is already up (e.g. a coexisting clash GUI), enabling TUN asks
whether to shut the other device down (via sudo `ip link delete`), enable ours anyway
(two TUNs fight over routes/DNS — not recommended), or cancel.
