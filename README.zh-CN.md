# mihomo-king

[English](README.md) | **简体中文**

一个轻量的 Linux TUI，用于管理 [mihomo](https://github.com/MetaCubeX/mihomo)
（Clash.Meta）、[sing-box](https://github.com/SagerNet/sing-box) 或
[Xray](https://github.com/XTLS/Xray-core) 代理核心，使用 Go 和
[Bubble Tea](https://github.com/charmbracelet/bubbletea) 构建。

## 功能

- 下载订阅或导入本地文件：支持 Clash/mihomo YAML、Base64 节点列表，以及每行一个节点链接。
- 从分享链接添加本地节点（vmess、vless、ss、trojan、hysteria2、socks5、http）。
- 订阅页支持多级分组。其他订阅和自建组排在前面，其后依次是**本地导入配置文件**和**本地导入单个节点**。按 `f` 在当前层新建组，`Enter` 进入，`Esc` 返回。按空格可跨组勾选条目，`Ctrl+A` 勾选或取消当前可见条目。按 `m` 选择目标组，`v` 确认移动，`Esc` 取消并保留勾选。
- 订阅页按 `t` 切换树形与分组列表。树中按 `→` 展开或进入子项，`←` 收起或返回父组。按 `s` 选择默认、名称升序、名称降序或手动排序。`K` / `J` 上下移动当前同级条目并启用手动排序。视图和排序偏好会在重启后保留。
- 切换当前订阅并重新加载代理核心。
- 在「设置 → 启动与运行」选择管理 mihomo、sing-box 或 Xray。
- 在「设置 → 内核管理 → 内核更新」开启「内核更新提醒」后，TUI 会在启动时检查当前管理的内核，并在运行期间定时检查。检查间隔默认 `1d`，也可填写 `12h`、`30m` 等。发现更新时，按 `F2` 可关闭右上角的版本提醒。
- 将 Clash/mihomo 订阅转换为 sing-box 或 Xray 运行配置。
- 开启或关闭 TUN 模式。
- 在策略组中切换节点。
- 创建入口到出口的链式代理，并在手动策略组中选择它。
- 测试单个节点或整个策略组的延迟，支持自定义测速地址。

主界面显示当前运行时的节点选择和协议类型，嵌套策略组会展开到最终节点。
规则模式默认显示主策略组（mihomo 使用配置顺序中的首组，sing-box 使用默认出站），按 `Enter` 展开或收起其他规则，全局模式显示 `GLOBAL` 的选择，直连模式显示 `DIRECT`。
按 `R` 重启当前管理的核心（停止时会启动），按 `r` 刷新状态；状态也会每 3 秒自动刷新。
每次进入主界面或按 `r` 时，会显示当前节点和直连到同一测速地址的延迟，默认地址为 Google。展开后才检测其他规则的节点。
多个策略组选中同一节点时只测试一次。「设置 → 延迟测试」中的「主界面延迟显示」默认开启，关闭后不显示或自动检测延迟；
「延迟刷新间隔」默认 `10s`，支持 `30s`、`1m` 等时长，最小 `1s`。两项设置在重开程序后保持。
停留在主界面时按间隔重新检测；上一轮未完成时不会叠加请求，离开主界面后停止自动检测。
mihomo 和 sing-box 通过内核的 `DIRECT` 出站测直连。Xray 通过本机网络测直连，遵循操作系统路由。

### 界面语言

界面默认使用英文，任意页面按 `Ctrl+L` 切换中文或英文。
菜单、帮助、表单和弹窗会一起切换，保留当前输入、筛选条件和选择；密码框中也可使用该快捷键。
语言选择会保存到应用目录下的 `language` 文件，重开程序后仍然有效。

### 开机启动

打开设置页（`4`），进入「启动与运行」，选中「开机启动」，按 `Enter` 切换；英文界面显示为 `autostart`。
该功能**默认关闭**。开启后，在下次登录 Linux 桌面时，按照保存的核心、订阅和 TUN 设置后台启动代理，
不弹出 TUI。关闭只取消自动启动，不停止正在运行的代理。

开关保存在 `${XDG_CONFIG_HOME:-~/.config}/autostart/mihomo-king.desktop`。
请从固定位置的可执行文件开启，勿使用 `go run`。移动程序后需重新关闭、开启，以更新路径。
如果启用了 TUN，请提前授予核心相应权限。

## 构建与运行

```bash
make build          # Linux glibc 构建（默认）
make build LIBC=musl # 适用于 musl 系统的静态构建
./mihomo-king       # 不带参数时打开 TUI
make install        # 程序安装到 ~/.local/bin，补全脚本安装到 ~/.local/share
```

`make build` 默认使用 glibc。`LIBC=musl` 关闭 CGO，生成不依赖系统 libc 的静态程序，可在 musl 发行版上运行。

### NixOS

Flake 支持在 `x86_64-linux` 和 `aarch64-linux` 上从源码构建，附带 Bash 和 Zsh 补全。
启用 flakes 后，可以直接打开 TUI，也可以安装到当前用户的 profile。

```bash
nix run github:jokergaming/mihomo-king
nix profile add github:jokergaming/mihomo-king
```

要在 NixOS 上全局安装，请在系统 flake 中添加输入，并在 `environment.systemPackages` 中引用软件包。

```nix
# flake.nix
inputs.mihomo-king.url = "github:jokergaming/mihomo-king";
```

```nix
# 放入 nixosSystem 的 modules 中，此处 inputs 为系统 flake 的输入。
({ pkgs, ... }: {
  environment.systemPackages = [
    inputs.mihomo-king.packages.${pkgs.stdenv.hostPlatform.system}.default
    pkgs.mihomo
  ];
})
```

代理核心需单独安装。通过 Nix 安装的核心由 NixOS 配置更新。
如需使用应用内的核心更新或 `setcap` 授权，请将核心配置到可写路径。
NixOS 上的 `pkgs.libcap` 提供 `setcap` 和 `getcap`，`pkgs.iproute2` 提供 `ip`。

## 命令行

常用的 TUI 操作也提供命令行子命令，基于 [cobra](https://github.com/spf13/cobra) 实现。
使用 `mihomo-king <command> --help` 查看详细说明。

```text
信息：status [--json] · version · paths [settings|subs|dir|config|log|pid|bin]
      subs [--urls] · check [sub] · log [-n N] [-f]
控制：start · stop · restart · use <sub> · update [--all] [--apply] [sub...]
      tun [on|off|toggle] [--force] · mode [rule|global|direct]
      core download [mihomo|sing-box|xray] [--channel stable|alpha]
代理：groups [-s] · nodes <group> · select <group> <node>
      chain [<entry> <exit>|off]
      delay [--url URL] [--timeout 5s] <group|node> [node]
```

```bash
mihomo-king -v                        # 程序版本；version 子命令还会显示构建和核心信息
mihomo-king status
mihomo-king use provider-a            # 先校验配置，加载失败时回滚
mihomo-king core download             # 替换已配置路径下的代理核心
mihomo-king core download xray --channel alpha
mihomo-king select Proxy "HK 02"
mihomo-king delay Proxy               # 测试组内全部节点，按延迟排序
mihomo-king chain "HK 01" "US 02"      # 通过 HK 01 连接 US 02
mihomo-king select Proxy "mihomo-king Chain" # 让策略组使用链式代理
mihomo-king chain off                 # 删除生成的链式代理节点
tail -f "$(mihomo-king paths log)"
```

同一内核的两个渠道共用安装目录。例如，从 `PATH` 找到 `/opt/bin/xray` 后，Alpha 版安装到 `/opt/bin/xray-alpha`。两个渠道均未安装且未指定路径时，首次下载使用 `~/.local/bin/<内核名>` 或 `~/.local/bin/<内核名>-alpha`。停止内核后可在「启动与运行」切换「内核渠道」。下载另一渠道时，当前渠道可以继续运行。`--channel release` 仍可作为 `stable` 的别名。

每种内核默认使用各自的官方 GitHub Releases。「内核发布地址」可为当前管理的内核设置 GitHub 仓库发布页，或返回 GitHub Releases 列表 JSON 的 HTTPS 地址。下载和更新提醒均使用该设置，清空后恢复官方源。

更新时先下载并检查新内核。当前渠道正在运行时，程序会先用新内核校验现有配置，再备份旧文件、停止内核、在原路径替换并重启。重启失败会自动恢复旧文件并重新启动。Linux 按需提示输入 sudo 密码以设置网络 capabilities。每个路径和渠道默认保留最近 3 份备份，可在设置中调整；设为 0 时，成功更新后清理备份。手动恢复时在「内核管理 → 内核备份 → 恢复内核备份」中按 UTC 时间选择，也可使用命令行：

```bash
mihomo-king core backups
mihomo-king core restore              # 恢复当前渠道最近的备份
mihomo-king core restore xray --channel alpha --backup <时间戳>
```

链式代理使用当前订阅或本地节点中的两个具体节点。
每个当前订阅只保存一条链式代理，并将其加入手动策略组供选择；切换订阅不会把该链应用到其他订阅。

### Shell 补全

补全支持命令、参数以及动态查询的订阅名、策略组和节点名。

```bash
# bash
mihomo-king completion bash > ~/.local/share/bash-completion/completions/mihomo-king
# zsh：目录必须在 compinit 执行前加入 $fpath
mihomo-king completion zsh > "${fpath[1]}/_mihomo-king"
```

## 发布

GitHub Actions 为 Linux 和 macOS 的 amd64、arm64 架构生成发布产物。
Linux 每种架构同时提供 glibc 动态链接版本和兼容 musl 的静态版本，发布流程会检查两者的链接方式。
正式版本标题与 `vX.X` 或 `vX.X.X` 标签一致。创建并推送版本标签即可发布稳定版，例如使用 Jujutsu：

```bash
jj tag set vX.X -r main
jj git push --bookmark main --tag vX.X
```

每次推送到 `main` 都会更新同一个 `nightly` 预发布版本。
流程会移动非版本号标签 `nightly`，不会将它设为最新稳定版，也不需要创建新的版本标签。
手动运行工作流时，标签留空表示更新 nightly；填写已有的版本标签则重新构建该版本。

发布产物包含以下平台的 `tar.gz` 压缩包和对应的 SHA-256 校验文件：

- `linux-amd64-glibc`（默认）
- `linux-amd64-musl`
- `linux-arm64-glibc`（默认）
- `linux-arm64-musl`
- `darwin-amd64`（Intel 芯片 Mac）
- `darwin-arm64`（Apple 芯片 Mac，M 系列）

程序将选中的核心作为后台子进程管理，在主界面启动或停止它，并通过兼容 Clash 的 API 进行实时控制。
应用设置保存在 `${XDG_CONFIG_HOME:-~/.config}/mihomo-king/`；mihomo 的运行配置保存在
`~/.config/mihomo/config.yaml`，sing-box 的运行配置保存在 `~/.config/sing-box/config.json`。

## TUN 权限

Linux 上创建 TUN 设备需要 `CAP_NET_ADMIN` 和 `CAP_NET_RAW` 权限。
在主界面启用 TUN 时，如果代理核心缺少这些权限，程序会在界面内提示输入 sudo 密码（隐藏显示），
执行 `setcap` 后重启核心使权限生效。也可以提前手动授予权限：

```bash
sudo setcap cap_net_admin,cap_net_raw=ep "$(command -v mihomo)"
sudo setcap cap_net_admin,cap_net_raw=ep "$(command -v sing-box)"
```

Linux 上如果其他 TUN 设备已经启用，例如同时运行的 Clash 图形客户端，程序会询问是否使用 sudo 关闭其他设备、
仍然启用本程序的 TUN，或取消操作。多个 TUN 会争用路由和 DNS，不建议同时开启。

**macOS** 不支持 `setcap`，代理核心必须以 root 运行。默认设备名为 `utun1024`；
已有配置需在设置中使用未被占用的 `utun<数字>` 设备名。
保留原用户的配置路径，重启核心后打开界面：

```bash
sudo env HOME="$HOME" XDG_CONFIG_HOME="${XDG_CONFIG_HOME:-$HOME/.config}" "$(command -v mihomo-king)" restart
sudo env HOME="$HOME" XDG_CONFIG_HOME="${XDG_CONFIG_HOME:-$HOME/.config}" "$(command -v mihomo-king)"
```

CLI 的 `tun on`、`tun off`、`stop` 等命令也使用相同前缀。
仅执行 `sudo mihomo-king tun on` 无法提升已由普通用户启动的核心权限，必须先重启核心。
程序不会自动删除 macOS 系统的 `utun` 接口；如有其他 VPN，请先在对应应用中关闭其 TUN。
TUN 切换失败会恢复之前的设置，macOS 上仅在配置的接口确实启动后才显示 TUN 已开启。

## Star History

<a href="https://www.star-history.com/?repos=jokergaming%2Fmihomo-king&amp;type=date&amp;legend=top-left">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/svg?repos=jokergaming/mihomo-king&amp;type=Date&amp;theme=dark" />
    <source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/svg?repos=jokergaming/mihomo-king&amp;type=Date" />
    <img alt="mihomo-king 的 Star History 图表" src="https://api.star-history.com/svg?repos=jokergaming/mihomo-king&amp;type=Date" width="600" />
  </picture>
</a>
