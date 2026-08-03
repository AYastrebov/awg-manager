# Tailscale support in awg-manager — design

Date: 2026-08-03
Branch: `feat/tailscale-support`
Status: approved design, ready for implementation planning

## 1. Goal

Join the Keenetic router to a Tailscale tailnet so that:

1. Any tailnet client reaches the router and the devices behind it (subnet router).
2. The router can act as an **exit node**, and the operator chooses where that
   egress traffic leaves: the WAN, or any AWG / sing-box tunnel already managed
   by awg-manager.
3. The operator sees tailnet peers and node state in the web UI.

Feature parity target is the OpenWrt tailscale guide, not a bespoke design.

## 2. Rejected approach: libtailscale / tsnet

The original idea was `github.com/tailscale/libtailscale`. It does not fit.

`libtailscale` is a C binding over Go's `tailscale.com/tsnet` — an in-process
**userspace** node that can only `Listen`/`Dial` on behalf of the embedding
process. It never touches the host routing table, and it supports neither
`--advertise-routes` nor `--advertise-exit-node`. Embedding it would deliver
"the awg-manager web UI is reachable at a tailnet IP" and nothing else, while
dragging the very large `tailscale.com` dependency tree into a `go.mod` that
today has ten direct requirements.

**Decision: run the real `tailscaled` daemon as a managed external binary**, the
same way sing-box is run today.

## 3. Platform verification: TUN on Keenetic

Exit node and subnet routing both require a real TUN device;
`--tun=userspace-networking` supports neither. TUN availability was verified
from the existing codebase rather than assumed:

- `internal/singbox/process.go:63` and `:558` record a stand-verified
  (2026-06-17) failure where sing-box re-opening the tun on SIGHUP fails with
  `TUNSETIFF: device or resource busy`. `TUNSETIFF` is the `/dev/net/tun`
  ioctl; an `EBUSY` response means the ioctl reached the tun driver. The
  workaround (`ReloadNeedsRestart` → full restart instead of SIGHUP) is
  production code.
- fakeip-tun is a shipped feature: `internal/storage/types.go:127`,
  `internal/singbox/router/config_fakeip.go:94` builds a real
  `{"type":"tun","interface_name":"opkgtun10","stack":"gvisor"}` inbound.
- Keenetic hands out tun slots deliberately.
  `internal/ndms/command/interfaces.go:33` `CreateOpkgTunWithSecurityLevel`
  registers NDMS `OpkgTunN` interfaces that materialise as kernel `opkgtunN`
  devices; `internal/tunnel/backend/kernel.go:64` notes that *NDMS itself*
  recreates `opkgtun*` devices as plain `tun` at boot.

Conclusion: TUN works. No new kernel module, no `tun.ko` packaging.

Caveat: no Go code in this repo opens `/dev/net/tun` directly — sing-box and
NDMS do — and only the `gvisor` stack is exercised today. A `stat` preflight is
kept as cheap insurance.

## 4. Interface strategy

**OS 5.x — take an NDMS `OpkgTunN` slot.** `tailscaled --tun=opkgtunN` instead
of a bare `tailscale0`, so NDMS sees the interface and firewall ACLs and
security level apply to it. Sequence copied from
`internal/wdtt/ndms_iface.go:177-215`:

1. `CreateOpkgTunWithSecurityLevel(ndmsName, description, "private")` +
   `SetMTU` — **no address**. Setting `ip address` before the kernel device
   exists wedges `ndm` in an nginx-reload loop (stand-verified 2026-07-15,
   PR #544).
2. Start `tailscaled`; wait for `opkgtunN` to appear in `/sys/class/net`.
3. `SetPermitAllACL` + `InterfaceUp`.

Index allocation goes through the existing `LiveOpkgTunIndices` collision check
(`internal/wdtt/ndms_iface.go:36`) so a tailscale slot cannot stomp a tunnel or
a WDTT server.

**OS 4.x — no NDMS.** Fall back to a plain `tailscale0`, mirroring how
`internal/tunnel/ops` splits OS4/OS5 while sharing the pure-kernel operations.

Two items require a bench spike before implementation (see §11): whether
`tailscaled` will attach to an NDMS-pre-created device or insists on creating
it itself, and whether NDMS `SetAddress` should be fed tailscale's own
`100.x.y.z/32` or skipped entirely.

## 5. Package layout

Mirrors `internal/singbox`, which is the closest existing analogue (managed
external binary + supervised process + UI).

```
internal/tailscale/
  installer/
    embedded.go        # generated: RequiredVersion + per-arch BinarySpec
    installer.go       # download, verify SHA256, space gating
    install_state.go   # reuses the singbox InstallState vocabulary
    proc_sweep.go      # kill foreign tailscaled before taking over
  operator.go          # lifecycle: preflight → install → start → apply → watch
  operator_process.go  # childproc spawn/stop/restart
  watchdog.go          # periodic liveness + restart backoff
  restart_backoff.go
  localapi.go          # HTTP over unix socket, GET /localapi/v0/status
  cli.go               # argv builders for up/set/down/logout, login-URL capture
  egress.go            # ip rule / routing table / iptables NAT reconciler
  ndms_iface.go        # OpkgTun slot allocation + lifecycle (OS5)
  preflight.go         # /dev/net/tun, disk space, port, foreign daemon
  store.go             # persisted desired state (tailscale.json)
  logbuffer.go         # ring buffer for UI, with auth-key scrubbing
  types.go
internal/api/tailscale*.go
cmd/awg-manager/wiring_tailscale.go
cmd/awg-manager/tailscale_adapters.go
frontend/src/routes/tailscale/
```

## 6. Binary distribution

Same pattern as `internal/singbox/installer`. Upstream publishes static
`mipsle` / `mips` / `arm64` builds, so no fork is needed — the release script
repacks them onto `repo.hoaxisr.ru`.

- `embedded.go` is generated by `scripts/regen-embedded-tailscale.sh`, carrying
  `RequiredVersion` and a `map[string]BinarySpec{Version, URL, SHA256, Size}`
  keyed `mipsel-3.4` / `mips-3.4` / `aarch64-3.10`, exactly like
  `internal/singbox/installer/embedded.go`.
- The combined `tailscaled` binary (~35 MB) plus the `tailscale` CLI are
  installed under `/opt/etc/awg-manager/tailscale/`.
- Reuses `internal/downloader` and the existing `InstallState` vocabulary
  (`installed`, `missing`, `missing_no_space`, `outdated_no_space`,
  `installing`, `error`) so the UI banner logic and the no-space gates come for
  free.

## 7. Process model

- Spawn via `internal/childproc` (pid file + stderr ring buffer), as sing-box
  does.
- Watchdog + `restart_backoff` mirroring `internal/singbox/watchdog.go`.
- A sticky "manually stopped" flag persisted through the settings store so a
  user-pressed Stop survives an awg-manager restart, mirroring
  `SetManuallyStopped` in `cmd/awg-manager/wiring_singbox.go`.

Daemon invocation:

```
tailscaled --statedir=/opt/etc/awg-manager/tailscale \
           --socket=/opt/var/run/tailscale/tailscaled.sock \
           --tun=opkgtunN --port=41641
```

State application:

```
tailscale --socket=... up \
  --accept-dns=false \
  --advertise-routes=<lan prefixes> \
  --snat-subnet-routes=true \
  --hostname=<router hostname> \
  [--advertise-exit-node] [--login-server=<url>] [--authkey=<key>]
```

Bracketed flags are emitted only when the corresponding persisted state calls
for them; `cli.go` builds argv from the store, so "desired state" has exactly
one source of truth.

**`--accept-dns=false` is forced in v1.** `tailscaled` would otherwise rewrite
`/etc/resolv.conf`, which NDMS owns and rewrites back — a guaranteed fight.
Consequence, stated in the UI: MagicDNS names do not resolve *on the router
itself*; tailnet clients are unaffected.

## 8. Control plane

Split, per the sing-box precedent of "structured reads, explicit writes":

- **Reads — LocalAPI.** `GET /localapi/v0/status` over the unix socket with
  `Host: local-tailscaled.sock`. Polled for the status view: backend state
  (`NeedsLogin` / `Starting` / `Running` / `Stopped`), self node address and
  hostname, and per-peer hostname, tailnet IPs, online flag, direct-vs-DERP
  relay, rx/tx counters, last handshake.
- **Writes — CLI.** `tailscale up/set/down/logout` with argv built in `cli.go`.
  Avoids reimplementing the unstable LocalAPI write surface.

Auth supports **both** paths: run `up`, scrape the
`https://login.tailscale.com/a/…` URL from stdout and surface it in the UI as a
clickable link plus QR; and accept a pasted pre-auth key (`tskey-auth-…`) for
headless provisioning. A custom control server is supported via an optional
`--login-server` field (Headscale).

Auth keys are scrubbed from every log path, following
`internal/singbox/log_sanitize.go`.

**Route approval.** Advertised subnet routes and exit-node status require
approval in the Tailscale admin console. LocalAPI reports what is advertised
versus what is approved; the UI must show a "routes advertised, pending
approval" state rather than silently looking healthy.

## 9. Egress and NAT

The riskiest surface. Two modes, selected in the UI.

**WAN egress (default).** `net.ipv4.ip_forward=1`, `FORWARD` accept for the
tailscale interface both directions, `MASQUERADE` out the current WAN device.
Implemented as a direct analogue of `internal/wdtt/entware_nat_linux.go`,
including the `defaultWANDev` re-resolution: after a WAN failover the rule
still carries its comment but points at a dead interface, so presence checks
compare `masqueradeOutDev(...)` against the live default-route device and
reinstall on mismatch. Rules are tagged with a dedicated comment constant
(`AWGM_TAILSCALE`, alongside `AWGM_WDTT`).

**Tunnel egress.** Reuses the client-route policy-routing primitives in
`internal/tunnel/ops/clientroute.go` rather than inventing a parallel
mechanism:

- `ListUsedRoutingTables` → allocate a table (same collision-avoidance path as
  `clientroute`).
- `SetupClientRouteTable(kernelIface, table)` — default route via the selected
  tunnel plus the LAN bypass route.
- `AddClientRule("100.64.0.0/10", table)` — `ip rule from 100.64.0.0/10 lookup
  <table>`, steering all tailnet-sourced traffic into it.
- `MASQUERADE` out the tunnel interface.

**Tunnel-down behaviour** reuses the `clientroute` fallback vocabulary
(`internal/clientroute/impl.go:228`): `drop` keeps the `ip rule` in place as a
kill switch so exit-node traffic is blackholed rather than leaking to the WAN;
`bypass` removes it and traffic follows the default route. Default is `drop`,
because an exit node silently falling back to the bare WAN is precisely the
failure users would not notice.

**Reconciliation.** NDMS and the sing-box router reconcile loop both flush
`POSTROUTING`/`FORWARD` rules. A periodic reconciler on the
`internal/wdtt/nat_reconcile.go` model (15 s ticker, presence check, reinstall
on drift) keeps the rules alive, and also reaps an orphaned `OpkgTunN` slot if
the daemon is gone.

## 10. API, UI, storage

**Storage.** `internal/tailscale/store.go` writing `tailscale.json` in the data
dir, on the `internal/wdtt/store.go` model: RWMutex, cached config, normalize
on load, copy-on-return so handler mutations cannot leak into the cache, atomic
write via `internal/storage/atomic.go`. Persisted: enabled flag, login server,
hostname, advertised routes, exit-node flag, egress mode (`wan` | `tunnel`),
egress tunnel id, tunnel-down fallback, allocated OpkgTun index, allocated
route table, manually-stopped flag. The pre-auth key is write-only from the
API's perspective — stored for re-provisioning, never returned to the client.

**API** (`internal/api/tailscale*.go`, registered in
`internal/server/server_routes.go` next to the wdtt block, all behind
`h.guarded`):

| Route | Purpose |
|---|---|
| `GET /api/tailscale/status` | backend state, self node, install state, route-approval state |
| `GET /api/tailscale/peers` | peer list from LocalAPI |
| `GET /api/tailscale/config` | persisted desired state (no secrets) |
| `POST /api/tailscale/config` | update desired state, re-apply |
| `POST /api/tailscale/install` | install / update the managed binary |
| `POST /api/tailscale/up` | start + auth, returns login URL when interactive |
| `POST /api/tailscale/down` | stop (sets manually-stopped) |
| `POST /api/tailscale/logout` | drop node identity, clear state |
| `GET /api/tailscale/logs` | ring buffer, scrubbed |

**Frontend.** New `frontend/src/routes/tailscale/` page following the existing
route conventions: install/no-space banner, connect card (login URL + QR or
auth key), advertised-routes editor pre-filled from detected LAN prefixes,
exit-node toggle with egress selector (WAN or tunnel picker + fallback), peer
table reusing the connection-list presentation, and a log pane.

**Events.** Status transitions publish on the existing `internal/events` bus so
the UI updates without polling the heavy endpoints.

## 11. Preflight and diagnostics

Blocking gate before enabling, each failure rendering an actionable message
rather than a retry loop:

- `/dev/net/tun` present and openable.
- Free space sufficient for the pinned binary (existing `downloadByteLimit`
  logic).
- No foreign `tailscaled` already running (`proc_sweep` pattern).
- A free `OpkgTunN` index available (OS 5.x).
- UDP 41641 not already bound.

## 12. Open questions for the implementation spike

1. Does `tailscaled --tun=opkgtunN` attach to an NDMS-pre-created device, or
   must it create the device and let NDMS adopt it (as sing-box and the WDTT
   server do)?
2. Should NDMS `SetAddress` receive tailscale's `100.x.y.z/32`, or be skipped
   and left entirely to `tailscaled`?
3. Does the pinned tailscale build run on the mipsel kernel 3.4 target, or does
   it need the same fork-and-rebuild treatment sing-box got?

Each is a bench check, not a design fork — the answers change a call sequence,
not the architecture.

## 13. Testing

Matching the repo's existing style — pure functions tested directly, external
commands behind injectable runners:

- `cli.go` — argv construction per state combination; login-URL extraction from
  captured stdout fixtures; auth-key scrubbing.
- `localapi.go` — status parsing from recorded JSON fixtures, including the
  advertised-but-unapproved routes case.
- `egress.go` — rendered iptables and `ip rule` argv for both egress modes;
  drift detection (`masqueradeOutDev` mismatch after WAN failover); `drop` vs
  `bypass` on tunnel down.
- `installer` — arch selection, SHA mismatch, space gating, `InstallState`
  classification.
- `ndms_iface.go` — slot allocation collision avoidance against live indices;
  ordering assertion that no address is set before the kernel device appears.
- `store.go` — normalize/copy-on-return isolation, secret never serialised
  outward.
- Operator lifecycle with fake process, fake CLI runner and a fake unix-socket
  LocalAPI server.

## 14. Explicitly out of scope for v1

`--accept-routes` (pulling in other nodes' subnets — clashes with NDMS-managed
routing), Tailscale SSH, Funnel/Serve, MagicDNS on the router, IPv6 exit-node
egress, and per-peer ACL editing.
