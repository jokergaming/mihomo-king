# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project status

MVP implemented. The package layout below exists and builds (`go build .`). The tool
launches mihomo as a background subprocess and exposes four screens (dashboard,
subscriptions, nodes) over the Bubble Tea runtime. Keep changes coherent with the
architecture notes here.

`mihomo-king` is a small Linux TUI for managing a [mihomo](https://github.com/MetaCubeX/mihomo)
(Clash.Meta) proxy. Scope is deliberately small:

- **Subscriptions** — download a subscription URL, store it, list stored subscriptions.
- **Switch subscription** — make a stored subscription the active config and reload mihomo.
- **TUN toggle** — turn TUN mode on/off.
- **Switch node** — pick the active node within a proxy group.

Keep it simple. Resist adding rule editors, connection viewers, or provider management unless asked.

## Tech stack

- **Go** (1.26 installed). Single static binary.
- **Charm TUI stack**: `bubbletea` (Elm-architecture runtime), `bubbles` (list / textinput /
  spinner / table / viewport widgets), `lipgloss` (styling). Don't pin to memory — check
  `go.mod` for the actual versions once it exists.
- **YAML**: `gopkg.in/yaml.v3` for reading/merging mihomo config.
- HTTP via stdlib `net/http`.

## Commands

```bash
go mod init mihomo-king          # once, first thing — module path can be adjusted later
go build -o mihomo-king .        # build
go run .                         # run from source
go test ./...                    # all tests
go test ./internal/config/ -run TestMerge -v   # single test
go vet ./...                     # vet
gofmt -w . && go mod tidy        # format + tidy deps
```

TUI debugging: stdout is the rendered UI, so `fmt.Println` is useless and `log` corrupts the
screen. Use `tea.LogToFile("debug.log", "")` (set `BUBBLETEA_LOG` or a flag to enable) and
`tail -f debug.log` in another terminal.

## How the TUI controls mihomo — the central design question

mihomo is controlled two ways, and this tool needs **both**:

1. **RESTful API** (`external-controller`) — live changes, no restart. Used for node switching,
   live TUN toggle, latency tests, reading current state.
2. **Config file + reload** — for swapping subscriptions wholesale (different proxies/rules).

**Critical gotcha:** the existing `~/.config/mihomo/config.yaml` has **no `external-controller`**.
Without it the API is unreachable. The tool must *ensure* the active config contains an
`external-controller` (e.g. `127.0.0.1:9090`) and a `secret` before it can do anything live.
Generate/persist a secret in app settings and inject it on every config write.

### mihomo RESTful API reference (base `http://127.0.0.1:9090`, `Authorization: Bearer <secret>`)

- `GET /version` — connectivity check; `{"version","meta":true}`.
- `GET /configs` — current runtime config (mode, tun, ports…).
- `PATCH /configs` — **live partial update**. `{"tun":{"enable":true}}` toggles TUN with no
  restart; `{"mode":"global"}` changes mode. Preferred for the TUN toggle.
- `PUT /configs?force=true` body `{"path":"/abs/config.yaml"}` — reload config from file
  (`force=true` also reloads providers). Use after writing a new active config (subscription switch).
- `GET /proxies` — `{"proxies":{"<name>":{type, now, all:[...], history}}}`. Groups have
  `type: Selector|URLTest|Fallback|LoadBalance`, `all` = member names, `now` = current pick.
  Only **Selector** groups are user-switchable.
- `PUT /proxies/{group}` body `{"name":"<node>"}` — select a node in a Selector group.
- `GET /proxies/{name}/delay?url=http://www.gstatic.com/generate_204&timeout=5000` — latency test.
- `GET /group/{name}/delay?...` — test all members of a group.
- `GET /logs` / `GET /traffic` — chunked streaming (optional, for a status view).

### Subscriptions

- A subscription URL usually returns a **full Clash/mihomo YAML config** (proxies + proxy-groups
  + rules, sometimes dns/rule-providers). Send a Clash-style `User-Agent` (e.g. `mihomo/1.19` or
  `clash.meta`) so the provider serves Clash format, not a raw node list.
- Parse the `subscription-userinfo` response header for quota/expiry to display:
  `upload=…; download=…; total=…; expire=<unix>`.
- Store each subscription's raw YAML by name; keep its URL + last-updated in app settings.

### Active-config strategy (decouple subscription / TUN / node)

Don't hand the raw subscription YAML to mihomo as-is — you'd lose your controller, secret, ports,
and TUN settings every switch. Instead **merge**:

- **From the subscription**: `proxies`, `proxy-groups`, `rules`, `rule-providers`, `proxy-providers`.
- **Injected/owned by this tool** (from app settings, overlaid on every write): `external-controller`,
  `secret`, `mixed-port`/`port`, `allow-lan`, `mode`, `log-level`, `tun`, optionally `dns`.

Write the merged result to mihomo's data dir as the active `config.yaml`, then reload via the API.
This keeps "which subscription", "is TUN on", and "which node" independent.

### Process & privilege model (TUN needs root)

**Decided & implemented: setcap + spawn.** The tool spawns mihomo as a detached
background subprocess (`Setpgid` so it outlives the TUI), tracks it via a PID file under
the app dir, and stops it with `SIGTERM` to the process group. TUN requires
`CAP_NET_ADMIN` + `CAP_NET_RAW`, granted once to the binary:
`sudo setcap cap_net_admin,cap_net_raw=ep <mihomo>`. The dashboard warns (via `getcap`)
when TUN is on but the caps are missing. `internal/mihomo/process.go` owns this.

Alternative not taken: a **systemd service** (more robust for a persistent proxy) — swap
out `internal/mihomo` for `systemctl` calls if that's ever preferred.

mihomo is always launched with its data dir so caches/GeoIP resolve:
`mihomo -d ~/.config/mihomo -f <active config.yaml>`.

## Suggested package layout (keep flat & simple)

```
main.go                  # entry: load settings, construct root model, tea.NewProgram(...).Run()
internal/
  tui/                   # Bubble Tea: root model + screen switching, per-screen models, lipgloss styles
  api/                   # mihomo RESTful client (version, configs, proxies, delay)
  subscription/          # download + parse + store subscriptions; subscription-userinfo parsing
  config/                # read/write/merge mihomo YAML; app settings (controller addr, secret, subs)
  mihomo/                # locate binary, start/stop/reload, privilege/service handling
```

Bubble Tea is The Elm Architecture: `Model` holds state, `Update(msg) (Model, Cmd)` is the only
place state changes, `View() string` renders. **All IO** (HTTP download, API calls, spawning
mihomo) must run inside a `tea.Cmd` returning a `tea.Msg` — never block `Update`. Use a single
root model with a `screen`/state enum that delegates `Update`/`View` to the active sub-model.

## File & state locations (XDG)

- App settings + stored subscriptions: `${XDG_CONFIG_HOME:-~/.config}/mihomo-king/`.
- mihomo data dir (reuse the existing one — it already has `cache.db`, `geoip.metadb`):
  `~/.config/mihomo/`. The merged active config is written here as `config.yaml`.

## Environment specifics

- mihomo binary: `/home/joker/.bin/cmd/mihomo` — Mihomo Meta **v1.19.24**, `with_gvisor` (so TUN
  `stack: gvisor` is available; current config uses `stack: system`).
- Current `~/.config/mihomo/config.yaml`: ports 7890/7891/7892, `mode: rule`, `tun.enable: true`
  (`auto-route`, `auto-detect-interface`, `strict-route`), **no `external-controller`** (see gotcha above).
