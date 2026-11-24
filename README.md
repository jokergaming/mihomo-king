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
- Organize the subscription view into nested groups. Other subscriptions and custom groups appear first, followed by **Imported configuration files** and **Imported individual nodes**. Press `f` to create a group, `Enter` to open it, and `Esc` to go back. Use Space to mark entries across groups or `Ctrl+A` to toggle all visible entries. Press `m` to choose a destination, then `v` to move. Press `Esc` to cancel and keep your selection.
- Press `t` to switch between folder and tree views. In the tree, `→` expands a group or enters its first child, and `←` collapses it or selects its parent. Press `s` for default, alphabetical (ascending or descending), or manual order. Press `K` / `J` to move a row up or down among its siblings and switch to manual order. View and sorting preferences persist across restarts.
- Switch the active subscription and reload the managed core.
- Choose mihomo, sing-box, or Xray in **Settings → Startup and runtime**.
- Enable **core update reminders** in **Settings → Core management → Core updates** to check the managed core on startup and while the TUI is open. The check interval defaults to `1d` and accepts values such as `12h` or `30m`. Press `F2` to dismiss the version shown in the upper-right corner.
- Convert Clash/mihomo subscriptions into sing-box or Xray runtime config.
- Toggle TUN mode.
- Switch the selected node within a proxy group.
- Create an entry-to-exit proxy chain and select it in a manual proxy group.
- Test node and group latency with a configurable test URL.

The dashboard shows runtime node selections and protocols, following nested
groups to their final nodes. Rule mode shows the primary group (the first configured
group for mihomo, the final outbound for sing-box). Press `Enter` to expand or
collapse the other rules. Global mode
shows `GLOBAL`, and direct mode shows `DIRECT`. Press `R` to restart the managed
core (or start it if stopped), and `r` to refresh. Status refreshes every 3 seconds.
Opening the dashboard or pressing `r` tests the selected nodes with the configured
test URL (Google by default) and shows direct latency to the same URL. Other rules
are tested while expanded. Shared nodes are tested once per round.
In **Settings → Latency tests**, **dashboard latency** controls whether latency is shown and tested
(on by default), and **latency refresh interval** sets automatic testing while
the dashboard is open (default `10s`; accepts durations such as `30s` or `1m`,
minimum `1s`). Both preferences persist across restarts. A new round waits for
the previous round to finish; leaving the dashboard stops automatic testing.
Mihomo and sing-box measure direct latency through the core’s `DIRECT` outbound.
Xray uses a host HTTP connection for the direct probe, following the OS routing table.

### Interface language

The interface defaults to English. Press `Ctrl+L` on any screen to switch between
English and Simplified Chinese. Menus, help, forms, and dialogs switch together;
current input, filters, and selections are preserved, including in password
prompts. The choice is saved in the app directory's `language` file.

### Desktop autostart

Open Settings (`4`), choose **Startup and runtime → autostart**, and press `Enter` to toggle it. Autostart
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

### NixOS

The flake builds `mihomo-king` from source for `x86_64-linux` and `aarch64-linux`
and includes Bash and Zsh completions. With flakes enabled, use `nix run` to open
the TUI or install the package into your user profile.

```bash
nix run github:jokergaming/mihomo-king
nix profile add github:jokergaming/mihomo-king
```

For a system-wide installation, add the input to your NixOS flake and reference
its package from `environment.systemPackages`.

```nix
# flake.nix
inputs.mihomo-king.url = "github:jokergaming/mihomo-king";
```

```nix
# Inside your nixosSystem modules, where inputs is your flake's inputs.
({ pkgs, ... }: {
  environment.systemPackages = [
    inputs.mihomo-king.packages.${pkgs.stdenv.hostPlatform.system}.default
    pkgs.mihomo
  ];
})
```

The core is installed separately. Update Nix-managed cores through your NixOS
configuration. For the built-in core updater or `setcap` workflow, configure a
core binary on a writable path. On NixOS, `pkgs.libcap` provides `setcap` and
`getcap`, and `pkgs.iproute2` provides `ip`.

## Command line

Every common TUI action is also a subcommand (built with
[cobra](https://github.com/spf13/cobra)); `mihomo-king <command> --help` shows details.

```text
Information:  status [--json] · version · paths [settings|subs|dir|config|log|pid|bin]
              subs [--urls] · check [sub] · log [-n N] [-f]
Control:      start · stop · restart · use <sub> · update [--all] [--apply] [sub...]
              tun [on|off|toggle] [--force] · mode [rule|global|direct]
              core download [mihomo|sing-box|xray] [--channel stable|alpha]
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

Both channels share one installation directory. For example, if Settings finds `/opt/bin/xray` on `PATH`, it installs alpha at `/opt/bin/xray-alpha`. With neither channel installed or configured, the first download uses `~/.local/bin/<core>` or `~/.local/bin/<core>-alpha`. Choose **Startup and runtime → core channel** in Settings while the core is stopped. Downloading the other channel leaves the running channel alone. `--channel release` remains an alias for `stable`.

Each core defaults to its official GitHub Releases source. **Core release source** overrides the selected core for downloads and reminders. Enter a GitHub repository's releases page or an HTTPS endpoint returning the GitHub releases list in JSON. Clear it to restore the default.

An update stages the new binary, checks its version, and validates the active configuration when the core is running. It backs up the previous executable, stops the running core, replaces it at its existing path and restarts it. A failed restart triggers automatic rollback. Linux downloads prompt for sudo when needed to grant network capabilities. Each path and channel keeps three backups by default; set the count to zero to discard backups after a successful update. Use **Core management → Core backups → restore core backup** in Settings to select a backup by UTC timestamp, or use the CLI:

```bash
mihomo-king core backups
mihomo-king core restore              # newest backup of the selected channel
mihomo-king core restore xray --channel alpha --backup <timestamp>
```

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
- `darwin-amd64` (Intel Mac)
- `darwin-arm64` (Apple silicon Mac, M series)

The tool manages the selected core as a background subprocess (start/stop from
the dashboard) and talks to its Clash-compatible API for live changes. It writes
its own settings to `${XDG_CONFIG_HOME:-~/.config}/mihomo-king/`, the active
mihomo config to `~/.config/mihomo/config.yaml`, and the active sing-box config
to `~/.config/sing-box/config.json`.

## TUN requires privileges

On Linux, creating a TUN device needs `CAP_NET_ADMIN`/`CAP_NET_RAW`. When you enable TUN in
the dashboard, the tool handles privileges itself: if the selected core binary
lacks the caps it asks for your sudo password (masked, in-TUI) and runs `setcap`
for you, restarting the managed core so the caps take effect. To grant them
ahead of time instead:

```bash
sudo setcap cap_net_admin,cap_net_raw=ep "$(command -v mihomo)"
sudo setcap cap_net_admin,cap_net_raw=ep "$(command -v sing-box)"
```

On Linux, if another TUN device is already up (e.g. a coexisting clash GUI), enabling TUN asks
whether to shut the other device down (via sudo `ip link delete`), enable ours anyway
(two TUNs fight over routes/DNS — not recommended), or cancel.

On **macOS**, `setcap` is unavailable: the core must run as root. The default
device is `utun1024`; existing settings must use an unused `utun<number>` name.
Restart the core and open the TUI with the original user's configuration paths:

```bash
sudo env HOME="$HOME" XDG_CONFIG_HOME="${XDG_CONFIG_HOME:-$HOME/.config}" "$(command -v mihomo-king)" restart
sudo env HOME="$HOME" XDG_CONFIG_HOME="${XDG_CONFIG_HOME:-$HOME/.config}" "$(command -v mihomo-king)"
```

Use the same prefix for CLI commands such as `tun on`, `tun off` and `stop`.
Running only `sudo mihomo-king tun on` does not elevate a core that was already
started by a regular user. macOS system `utun` interfaces are not automatically
deleted; turn off any other VPN's TUN in its own app before enabling this one.
Failed TUN changes restore the previous setting, and macOS reports TUN as on
only when the configured interface is up.

## Star History

<a href="https://www.star-history.com/?repos=jokergaming%2Fmihomo-king&amp;type=date&amp;legend=top-left">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/svg?repos=jokergaming/mihomo-king&amp;type=Date&amp;theme=dark" />
    <source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/svg?repos=jokergaming/mihomo-king&amp;type=Date" />
    <img alt="Star History chart for mihomo-king" src="https://api.star-history.com/svg?repos=jokergaming/mihomo-king&amp;type=Date" width="600" />
  </picture>
</a>
