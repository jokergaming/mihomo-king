# mihomo-king

[English](README.md) | **简体中文**

一个轻量的 Linux TUI，用于管理 [mihomo](https://github.com/MetaCubeX/mihomo)
（Clash.Meta）、[sing-box](https://github.com/SagerNet/sing-box) 或
[Xray](https://github.com/XTLS/Xray-core) 代理核心，使用 Go 和
[Bubble Tea](https://github.com/charmbracelet/bubbletea) 构建。

## 功能

- 下载订阅或导入本地文件：支持 Clash/mihomo YAML、Base64 节点列表，以及每行一个节点链接。
- 从分享链接添加本地节点（vmess、vless、ss、trojan、hysteria2、socks5、http）。
- 切换当前订阅并重新加载代理核心。
- 在设置页选择管理 mihomo、sing-box 或 Xray。
- 将 Clash/mihomo 订阅转换为 sing-box 或 Xray 运行配置。
- 开启或关闭 TUN 模式。
- 在策略组中切换节点。
- 创建入口到出口的链式代理，并在手动策略组中选择它。
- 测试单个节点或整个策略组的延迟，支持自定义测速地址。

主界面显示当前运行时的节点选择和协议类型，嵌套策略组会展开到最终节点。
规则模式分别列出各策略组，全局模式显示 `GLOBAL` 的选择，直连模式显示 `DIRECT`。
按 `R` 重启当前管理的核心（停止时会启动），按 `r` 刷新状态；状态也会每 3 秒自动刷新。

### 界面语言

界面默认使用英文，任意页面按 `Ctrl+L` 切换中文或英文。
菜单、帮助、表单和弹窗会一起切换，保留当前输入、筛选条件和选择；密码框中也可使用该快捷键。
语言选择会保存到应用目录下的 `language` 文件，重开程序后仍然有效。

### 开机启动

打开设置页（`4`），选中「开机启动」，按 `Enter` 切换；英文界面显示为 `autostart`。
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

## 命令行

常用的 TUI 操作也提供命令行子命令，基于 [cobra](https://github.com/spf13/cobra) 实现。
使用 `mihomo-king <command> --help` 查看详细说明。

```text
信息：status [--json] · version · paths [settings|subs|dir|config|log|pid|bin]
      subs [--urls] · check [sub] · log [-n N] [-f]
控制：start · stop · restart · use <sub> · update [--all] [--apply] [sub...]
      tun [on|off|toggle] [--force] · mode [rule|global|direct]
      core download [mihomo|sing-box|xray] [--channel release|alpha]
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

`core download` 从 GitHub 安装 mihomo、sing-box 或 Xray。`--channel release` 使用最新稳定版，
`--channel alpha` 使用最新预发布版：mihomo 的滚动 `Prerelease-Alpha`、sing-box 最新的 `alpha`，
或 Xray 最新的预发布版本。下载会替换已配置路径下的文件，保留自定义路径，并输出对应的 GitHub 发布页。

如果该核心正在运行，程序会先停止它，再替换文件并重新启动。
替换文件会清除 capabilities，因此程序会为新文件设置 `cap_net_admin,cap_net_raw=ep`；
当前用户不是 root 时，会要求输入 sudo 密码。

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
- `darwin-amd64`
- `darwin-arm64`

程序将选中的核心作为后台子进程管理，在主界面启动或停止它，并通过兼容 Clash 的 API 进行实时控制。
应用设置保存在 `${XDG_CONFIG_HOME:-~/.config}/mihomo-king/`；mihomo 的运行配置保存在
`~/.config/mihomo/config.yaml`，sing-box 的运行配置保存在 `~/.config/sing-box/config.json`。

## TUN 权限

创建 TUN 设备需要 `CAP_NET_ADMIN` 和 `CAP_NET_RAW` 权限。
在主界面启用 TUN 时，如果代理核心缺少这些权限，程序会在界面内提示输入 sudo 密码（隐藏显示），
执行 `setcap` 后重启核心使权限生效。也可以提前手动授予权限：

```bash
sudo setcap cap_net_admin,cap_net_raw=ep "$(command -v mihomo)"
sudo setcap cap_net_admin,cap_net_raw=ep "$(command -v sing-box)"
```

如果其他 TUN 设备已经启用，例如同时运行的 Clash 图形客户端，程序会询问是否使用 sudo 关闭其他设备、
仍然启用本程序的 TUN，或取消操作。多个 TUN 会争用路由和 DNS，不建议同时开启。

## Star History

<a href="https://www.star-history.com/?repos=jokergaming%2Fmihomo-king&amp;type=date&amp;legend=top-left">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/svg?repos=jokergaming/mihomo-king&amp;type=Date&amp;theme=dark" />
    <source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/svg?repos=jokergaming/mihomo-king&amp;type=Date" />
    <img alt="mihomo-king 的 Star History 图表" src="https://api.star-history.com/svg?repos=jokergaming/mihomo-king&amp;type=Date" width="600" />
  </picture>
</a>
