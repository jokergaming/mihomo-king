# mihomo-king

A small Linux TUI for managing a [mihomo](https://github.com/MetaCubeX/mihomo)
(Clash.Meta) proxy, built with Go and [Bubble Tea](https://github.com/charmbracelet/bubbletea).

## Features

- Download and store subscriptions (Clash/mihomo YAML).
- Switch the active subscription and reload mihomo.
- Toggle TUN mode.
- Switch the selected node within a proxy group.

## Build & run

```bash
go build -o mihomo-king .
./mihomo-king
```

The tool manages mihomo as a background subprocess (start/stop from the dashboard)
and talks to its RESTful API for live changes. It writes its own settings to
`${XDG_CONFIG_HOME:-~/.config}/mihomo-king/` and the active mihomo config to
`~/.config/mihomo/config.yaml`.

## TUN requires privileges

Creating a TUN device needs `CAP_NET_ADMIN`/`CAP_NET_RAW`. Grant them once to the
mihomo binary so a normal user can enable TUN:

```bash
sudo setcap cap_net_admin,cap_net_raw=ep "$(command -v mihomo)"
```

The dashboard warns when TUN is on but the binary appears to lack these capabilities.
