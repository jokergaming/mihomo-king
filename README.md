# mihomo-king

**English** | [简体中文](README.zh-CN.md)

A small Linux TUI for managing [mihomo](https://github.com/MetaCubeX/mihomo)
(Clash.Meta), [sing-box](https://github.com/SagerNet/sing-box), or
[Xray](https://github.com/XTLS/Xray-core) proxy cores,
built with Go and [Bubble Tea](https://github.com/charmbracelet/bubbletea).

## Features

- Download subscriptions or import them from local files: Clash/mihomo YAML,
  base64 node lists, or node links one per line.
- Add local nodes from share links (vmess, vless, ss, trojan, hysteria2, socks5, http).
- Switch the active subscription and reload the managed core.
- Choose mihomo, sing-box, or Xray from the settings screen.
- Convert Clash/mihomo subscriptions into sing-box or Xray runtime config.
- Toggle TUN mode.
- Switch the selected node within a proxy group.
- Create an entry-to-exit proxy chain and select it in a manual proxy group.
- Test node and group latency with a configurable test URL.

The dashboard shows runtime node selections and protocols, following nested
groups to their final nodes. Rule mode lists each group's selection, global mode
shows `GLOBAL`, and direct mode shows `DIRECT`. Press `R` to restart the managed
core (or start it if stopped), and `r` to refresh. Status refreshes every 3 seconds.

### Interface language

The interface defaults to English. Press `Ctrl+L` on any screen to switch between
English and Simplified Chinese. Menus, help, forms, and dialogs switch together;
current input, filters, and selections are preserved, including in password
prompts. The choice is saved in the app directory's `language` file.

### Desktop autostart

Open Settings (`4`), select `autostart`, and press `Enter` to toggle it. Autostart
is **off by default**. When enabled, the next Linux desktop login starts the proxy
in the background using the saved core, subscription, and TUN settings, without
opening the TUI. Disabling it leaves the current proxy running.

The switch is stored at
`${XDG_CONFIG_HOME:-~/.config}/autostart/mihomo-king.desktop`. Enable it from a
binary at a permanent location, rather than `go run`. If you move the binary,
toggle autostart off and on to update its path. Grant the core's TUN capabilities
in advance if TUN is enabled.

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
              core download [mihomo|sing-box|xray] [--channel release|alpha]
Proxies:      groups [-s] · nodes <group> · select <group> <node>
              chain [<entry> <exit>|off]
              delay [--url URL] [--timeout 5s] <group|node> [node]
```

```bash
mihomo-king -v                        # version (also: mihomo-king version for build + core info)
mihomo-king status
mihomo-king use provider-a            # validated with mihomo -t, rolled back if the reload fails
mihomo-king core download             # replace the managed core at its configured path
mihomo-king core download xray --channel alpha
mihomo-king select Proxy "HK 02"
mihomo-king delay Proxy              # every member, fastest first
mihomo-king chain "HK 01" "US 02"     # connect to US 02 through HK 01
mihomo-king select Proxy "mihomo-king Chain" # route that group through the chain
mihomo-king chain off                 # remove the generated chain node
tail -f "$(mihomo-king paths log)"
```

`core download` installs mihomo, sing-box, or xray from GitHub. `--channel release`
uses the latest stable release; `--channel alpha` uses the latest alpha build
(mihomo's rolling `Prerelease-Alpha` tag, sing-box's newest `alpha` tag, or
xray's newest prerelease). The file already configured for that core is replaced
in place, so a custom path stays custom. The command prints the GitHub release
page afterwards — open it to see what changed. If that core is running, it is
stopped before the file is replaced and started again afterwards. Replacing the
file clears capabilities, so the command then sets `cap_net_admin,cap_net_raw=ep`
on the installed binary, asking for a sudo password when it is not already root.

The chain uses two concrete nodes from the active subscription or local nodes.
One chain is saved at a time for the active subscription and becomes available
in its manual proxy groups. Switching subscriptions leaves the other
subscription's routing as is.

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
Release titles match their `vX.X` or `vX.X.X` tags. Create and push a version
tag to publish a stable release:

```bash
git tag v0.1.0
git push origin v0.1.0
```

Every push to `main` refreshes the `nightly` prerelease from that commit.
GitHub Releases require a tag, so the workflow moves a non-version `nightly`
tag internally; no version tag is needed. Run the workflow manually without a
tag input to refresh nightly, or provide an existing version tag to rebuild it.

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

## Star History

<a href="https://www.star-history.com/?repos=jokergaming%2Fmihomo-king&amp;type=date&amp;legend=top-left">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/svg?repos=jokergaming/mihomo-king&amp;type=Date&amp;theme=dark" />
    <source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/svg?repos=jokergaming/mihomo-king&amp;type=Date" />
    <img alt="Star History chart for mihomo-king" src="https://api.star-history.com/svg?repos=jokergaming/mihomo-king&amp;type=Date" width="600" />
  </picture>
</a>
