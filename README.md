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
make build          # Linux glibc build (default)
make build LIBC=musl # static Linux build for musl systems
./mihomo-king       # no arguments: open the TUI
make install        # binary to ~/.local/bin, bash/zsh completions to ~/.local/share
```

`make build` uses glibc by default. `LIBC=musl` disables CGO and produces a
static binary that runs on musl-based distributions without a libc dependency.

## Command line

Every common TUI action is also a subcommand (built with
[cobra](https://github.com/spf13/cobra)); `mihomo-king <command> --help` shows details.

```text
Information:  status [--json] · version · paths [settings|subs|dir|config|log|pid|bin]
              subs [--urls] · check [sub] · log [-n N] [-f]
Control:      start · stop · restart · use <sub> · update [--all] [--apply] [sub...]
              tun [on|off|toggle] [--force] · mode [rule|global|direct]
Proxies:      groups [-s] · nodes <group> · select <group> <node>
              delay [--url URL] [--timeout 5s] <group|node> [node]
```

```bash
mihomo-king -v                        # version (also: mihomo-king version for build + core info)
mihomo-king status
mihomo-king use provider-a            # validated with mihomo -t, rolled back if the reload fails
mihomo-king select Proxy "HK 02"
mihomo-king delay Proxy              # every member, fastest first
tail -f "$(mihomo-king paths log)"
```

### Shell completion

Completion covers commands, flags, and live values: subscription names, proxy
groups and their nodes (queried from the running core).

```bash
# bash
mihomo-king completion bash > ~/.local/share/bash-completion/completions/mihomo-king
# zsh (the directory must be on $fpath before compinit)
mihomo-king completion zsh > "${fpath[1]}/_mihomo-king"
```

## Release

GitHub Actions publishes release binaries for Linux and macOS on amd64 and arm64.
Each Linux architecture has a glibc-linked archive and a static musl-compatible
archive. The release workflow checks the linkage of both variants.
Create and push a version tag to publish a release:

```bash
git tag v0.1.0
git push origin v0.1.0
```

The release uploads `tar.gz` archives and SHA-256 checksum files for:

- `linux-amd64-glibc` (default)
- `linux-amd64-musl`
- `linux-arm64-glibc` (default)
- `linux-arm64-musl`
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
