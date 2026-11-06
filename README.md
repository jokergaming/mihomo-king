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
- Create an entry-to-exit proxy chain and select it in a manual proxy group.
- Test node and group latency with a configurable test URL.

主界面显示当前运行时的节点选择和协议类型，嵌套策略组会展开到最终节点；
规则模式分别列出各策略组，全局模式显示 `GLOBAL` 的选择，直连模式显示 `DIRECT`。
按 `R` 重启当前管理的核心（停止时会启动），按 `r` 刷新状态；状态也会每 3 秒自动刷新。

界面默认使用英文，任意页面按 `Ctrl+L` 切换中文或英文，语言选择会保存到应用目录下的 `language` 文件。
菜单、帮助、表单和弹窗会一起切换；输入框、密码框和筛选过程中也可使用该快捷键，保留当前输入及选择。

设置页（`4`）提供 `autostart`（中文界面显示「开机启动」），默认关闭，选中后按 `Enter` 切换。
开启后在下次登录 Linux 桌面时，按保存的订阅、核心和 TUN 设置后台启动代理，不弹出 TUI。
开关保存在 `${XDG_CONFIG_HOME:-~/.config}/autostart/mihomo-king.desktop`，重新打开程序仍会保留；
关闭只取消自动启动，不停止正在运行的代理。请从固定位置的可执行文件开启，勿使用 `go run`；
移动程序后需重新关闭、开启，以更新路径。需要 TUN 时，请提前授予核心相应权限。

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
