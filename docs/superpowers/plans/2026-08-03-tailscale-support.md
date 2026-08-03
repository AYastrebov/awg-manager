# Tailscale Support Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Join the Keenetic router to a Tailscale tailnet as a subnet router and exit node, managed entirely from the awg-manager web UI, with selectable egress (WAN or any managed tunnel).

**Architecture:** A managed external `tailscaled` binary (NOT libtailscale/tsnet — that is a userspace-only tsnet wrapper that cannot advertise routes or act as an exit node), downloaded and pinned exactly like the wdtt client, supervised via `internal/childproc`, attached to an NDMS `OpkgTunN` slot on OS 5.x. Reads come from tailscaled's LocalAPI unix socket; writes go through the `tailscale` CLI. Egress NAT/policy-routing reuses the existing `internal/wdtt` iptables reconciler and `internal/tunnel/ops` client-route primitives.

**Tech Stack:** Go 1.25 (stdlib only — no new modules), Svelte frontend, iptables/iproute2 via `internal/sys/exec` + `internal/sys/iptables`, NDMS RCI via `internal/ndms/command`.

**Design spec:** `docs/superpowers/specs/2026-08-03-tailscale-support-design.md`

## Global Constraints

- **No new Go dependencies.** `go.mod` stays at its current ten direct requirements. The repo vendors (`vendor/`); any `go get` is a plan violation.
- **Go version floor:** `go 1.25.0` (from `go.mod`).
- **Arch keys** are exactly `"mipsel-3.4"`, `"mips-3.4"`, `"aarch64-3.10"` (see `cmd/awg-manager/sysenv.go:23`). No other key is valid.
- **Comments in English; user-facing error strings in Russian**, matching `internal/wdtt` and `internal/childproc/install.go:37`.
- **Every external binary is SHA256-pinned** and installed through `childproc.Install` — never `os.Rename` of an unverified download.
- **`--accept-dns=false` is always passed.** tailscaled must never rewrite `/etc/resolv.conf`; NDMS owns it.
- **Auth keys (`tskey-…`) must never reach a log line, an API response, or the events bus.**
- **iptables rules carry the comment `AWGM_TAILSCALE`** so the reconciler can find them, mirroring `entwareNATComment = "AWGM_WDTT"` (`internal/wdtt/entware_nat_linux.go:16`).
- **Linux-only code goes behind `//go:build linux`** with an `_other.go` stub, matching `internal/wdtt/entware_nat_linux.go` / `entware_nat_other.go`. The test suite must pass on darwin.
- **Test command:** `go test ./internal/tailscale/...` (add packages as they appear). Full check before any PR: `go build ./... && go test ./...`.
- **Commit style:** conventional commits, e.g. `feat(tailscale): …`, matching `git log`.

## File Structure

| File | Responsibility |
|---|---|
| `internal/tailscale/types.go` | `Config`, `Status`, `Peer`, `EgressMode`, defaults |
| `internal/tailscale/store.go` | `tailscale.json` persistence: load/normalize/copy-on-return/save |
| `internal/tailscale/install.go` | pinned `EmbeddedBinaries`, `installOne`, version record |
| `internal/tailscale/preflight.go` | `/dev/net/tun`, disk, foreign daemon, port checks |
| `internal/tailscale/cli.go` | argv builders for `up`/`set`/`down`/`logout`, login-URL extraction |
| `internal/tailscale/scrub.go` | auth-key redaction for logs and API responses |
| `internal/tailscale/localapi.go` | unix-socket HTTP client + `/localapi/v0/status` parsing |
| `internal/tailscale/process.go` | spawn/stop/pid/log-ring supervision of `tailscaled` |
| `internal/tailscale/watchdog.go` | liveness loop + restart backoff |
| `internal/tailscale/ndms_iface.go` | OpkgTun slot allocation and lifecycle (OS 5.x) |
| `internal/tailscale/egress_linux.go` + `_other.go` | WAN MASQUERADE/FORWARD + tunnel policy routing |
| `internal/tailscale/egress_reconcile.go` | 15 s drift reconciler |
| `internal/tailscale/service.go` | orchestration: enable/disable/apply/status |
| `internal/api/tailscale.go` | HTTP handlers |
| `cmd/awg-manager/wiring_tailscale.go` | construction + autostart |
| `frontend/src/routes/tailscale/+page.svelte` | UI |

---

### Task 0: Bench spike — answer the three open questions

This task produces **written answers**, not code. Tasks 6 and 7 depend on them. Run on a real Keenetic router with awg-manager installed.

**Files:**
- Modify: `docs/superpowers/specs/2026-08-03-tailscale-support-design.md` §12

- [ ] **Step 1: Confirm the arch build runs at all**

Download the pinned upstream static build for the router's arch to `/opt/tmp/`, then:

```sh
/opt/tmp/tailscaled --version
```

Expected: a version string. A `SIGILL`/`Illegal instruction` on mipsel means the upstream build does not fit kernel 3.4 / softfloat MIPS and the binary needs the same fork-and-rebuild treatment sing-box got — record that in §12 question 3.

- [ ] **Step 2: Test whether tailscaled attaches to an NDMS-pre-created device**

Create an unused slot through the router's own RCI (pick a free index from `ip link | grep opkgtun`), then:

```sh
/opt/tmp/tailscaled --statedir=/opt/tmp/ts --tun=opkgtun18 --socket=/opt/tmp/ts.sock
```

Expected outcomes to distinguish:
- starts and `ip -d link show opkgtun18` shows a live tun → tailscaled **attaches**; §12 q1 answer is "attach".
- `TUNSETIFF: device or resource busy` → NDMS holds the fd; the daemon must create the device itself and NDMS adopts it, exactly like sing-box fakeip (`internal/singbox/router/fakeip_enable.go:215`).

- [ ] **Step 3: Determine whether NDMS needs the address**

With tailscaled running and authenticated, check whether the interface carries tailscale's address without any RCI `SetAddress`:

```sh
/opt/sbin/ip addr show opkgtun18
```

Record whether a `100.x.y.z/32` is present. If yes, §12 q2 answer is "skip SetAddress".

- [ ] **Step 4: Write the answers into the spec**

Replace §12's three questions with the observed answers and the date, matching the repo's "stand-verified YYYY-MM-DD" comment convention.

- [ ] **Step 5: Commit**

```bash
git add docs/superpowers/specs/2026-08-03-tailscale-support-design.md
git commit -m "docs(tailscale): record bench spike answers"
```

Note: `docs/` is gitignored (`.gitignore:42`). If the commit reports nothing to add, that is expected — leave the file untracked and paste the answers into the task's review comment instead.

---

### Task 1: Config types and store

**Files:**
- Create: `internal/tailscale/types.go`
- Create: `internal/tailscale/store.go`
- Test: `internal/tailscale/store_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `type Config`, `type EgressMode string`, `DefaultConfig() Config`, `NewStore(dataDir string) *Store`, `(*Store).Load() (Config, error)`, `(*Store).Save(Config) error`. Every later task reads desired state through these.

- [ ] **Step 1: Write the failing test**

```go
package tailscale

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestStore_LoadCreatesDefaults(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	cfg, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.EgressMode != EgressWAN {
		t.Errorf("EgressMode = %q, want %q", cfg.EgressMode, EgressWAN)
	}
	if cfg.TunnelFallback != "drop" {
		t.Errorf("TunnelFallback = %q, want drop", cfg.TunnelFallback)
	}
	if _, err := os.Stat(filepath.Join(dir, "tailscale.json")); err != nil {
		t.Errorf("defaults not persisted: %v", err)
	}
}

func TestStore_LoadCopyIsolatesCache(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	cfg, _ := s.Load()
	cfg.AdvertiseRoutes = []string{"192.168.1.0/24"}
	if err := s.Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, _ := s.Load()
	got.AdvertiseRoutes[0] = "10.0.0.0/8"
	again, _ := s.Load()
	if again.AdvertiseRoutes[0] != "192.168.1.0/24" {
		t.Errorf("mutation leaked into cache: %v", again.AdvertiseRoutes)
	}
}

func TestStore_NormalizeRejectsUnknownEgressMode(t *testing.T) {
	dir := t.TempDir()
	raw := `{"egressMode":"bogus","tunnelFallback":"","advertiseRoutes":null}`
	if err := os.WriteFile(filepath.Join(dir, "tailscale.json"), []byte(raw), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := NewStore(dir).Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.EgressMode != EgressWAN {
		t.Errorf("EgressMode = %q, want fallback to %q", cfg.EgressMode, EgressWAN)
	}
	if cfg.TunnelFallback != "drop" {
		t.Errorf("TunnelFallback = %q, want drop", cfg.TunnelFallback)
	}
}

func TestConfig_AuthKeyNeverMarshalsOutward(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AuthKey = "tskey-auth-secret"
	data, err := json.Marshal(cfg.Public())
	if err != nil {
		t.Fatal(err)
	}
	if string(data) == "" || contains(string(data), "tskey-auth-secret") {
		t.Errorf("auth key leaked into public payload: %s", data)
	}
}

func contains(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) &&
		(func() bool {
			for i := 0; i+len(needle) <= len(haystack); i++ {
				if haystack[i:i+len(needle)] == needle {
					return true
				}
			}
			return false
		})()
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tailscale/... -run TestStore -v`
Expected: FAIL — `no Go files` / undefined `NewStore`.

- [ ] **Step 3: Write minimal implementation**

`internal/tailscale/types.go`:

```go
// Package tailscale manages a pinned tailscaled daemon that joins the router
// to a Tailscale tailnet as a subnet router and (optionally) an exit node.
package tailscale

// EgressMode selects where exit-node traffic leaves the router.
type EgressMode string

const (
	// EgressWAN masquerades exit-node traffic out the current default-route
	// device, the same path an unpolicied LAN client takes.
	EgressWAN EgressMode = "wan"
	// EgressTunnel steers exit-node traffic into a managed AWG/sing-box
	// tunnel via a dedicated routing table.
	EgressTunnel EgressMode = "tunnel"
)

// TailnetCGNAT is the address range tailscale assigns to nodes; the tunnel
// egress ip rule matches on it.
const TailnetCGNAT = "100.64.0.0/10"

// Config is the persisted desired state. It is the single source of truth
// for the argv built in cli.go — nothing derives flags from live daemon state.
type Config struct {
	Enabled         bool       `json:"enabled"`
	Hostname        string     `json:"hostname,omitempty"`
	LoginServer     string     `json:"loginServer,omitempty"`
	AdvertiseRoutes []string   `json:"advertiseRoutes"`
	ExitNode        bool       `json:"exitNode"`
	EgressMode      EgressMode `json:"egressMode"`
	EgressTunnelID  string     `json:"egressTunnelId,omitempty"`
	// TunnelFallback mirrors the clientroute vocabulary: "drop" keeps the ip
	// rule as a kill switch when the tunnel dies, "bypass" removes it so
	// traffic follows the default route.
	TunnelFallback string `json:"tunnelFallback"`
	// AuthKey is write-only from the API's perspective — persisted for
	// re-provisioning, never returned by Public().
	AuthKey string `json:"authKey,omitempty"`
	// ManuallyStopped survives an awg-manager restart so the watchdog does
	// not resurrect a daemon the user deliberately stopped.
	ManuallyStopped bool `json:"manuallyStopped"`
	// OpkgTunIndex is the allocated NDMS slot (OS 5.x); 0 = unallocated.
	OpkgTunIndex int `json:"opkgTunIndex,omitempty"`
	// RouteTable is the allocated policy-routing table for tunnel egress.
	RouteTable int `json:"routeTable,omitempty"`
}

// PublicConfig is Config minus every secret, for API responses.
type PublicConfig struct {
	Enabled         bool       `json:"enabled"`
	Hostname        string     `json:"hostname,omitempty"`
	LoginServer     string     `json:"loginServer,omitempty"`
	AdvertiseRoutes []string   `json:"advertiseRoutes"`
	ExitNode        bool       `json:"exitNode"`
	EgressMode      EgressMode `json:"egressMode"`
	EgressTunnelID  string     `json:"egressTunnelId,omitempty"`
	TunnelFallback  string     `json:"tunnelFallback"`
	HasAuthKey      bool       `json:"hasAuthKey"`
	ManuallyStopped bool       `json:"manuallyStopped"`
}

// Public strips secrets. Every API path returns this, never Config.
func (c Config) Public() PublicConfig {
	return PublicConfig{
		Enabled:         c.Enabled,
		Hostname:        c.Hostname,
		LoginServer:     c.LoginServer,
		AdvertiseRoutes: append([]string(nil), c.AdvertiseRoutes...),
		ExitNode:        c.ExitNode,
		EgressMode:      c.EgressMode,
		EgressTunnelID:  c.EgressTunnelID,
		TunnelFallback:  c.TunnelFallback,
		HasAuthKey:      c.AuthKey != "",
		ManuallyStopped: c.ManuallyStopped,
	}
}

// DefaultConfig is what a fresh install starts from.
func DefaultConfig() Config {
	return Config{
		EgressMode:      EgressWAN,
		TunnelFallback:  "drop",
		AdvertiseRoutes: []string{},
	}
}
```

`internal/tailscale/store.go` — modeled on `internal/wdtt/store.go`:

```go
package tailscale

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/hoaxisr/awg-manager/internal/storage"
)

// Store persists Config as tailscale.json in the data dir. Load returns a
// deep-enough copy that handler mutations cannot leak into the cache before
// Save — same contract as wdtt.Store.
type Store struct {
	path string
	mu   sync.RWMutex
	cfg  *Config
}

func NewStore(dataDir string) *Store {
	return &Store{path: filepath.Join(dataDir, "tailscale.json")}
}

func (s *Store) Load() (Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cfg != nil {
		return copyConfig(*s.cfg), nil
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			cfg := DefaultConfig()
			if saveErr := s.saveLocked(cfg); saveErr != nil {
				return cfg, saveErr
			}
			return copyConfig(cfg), nil
		}
		return Config{}, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	normalizeConfig(&cfg)
	s.cfg = &cfg
	return copyConfig(cfg), nil
}

func (s *Store) Save(cfg Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked(cfg)
}

func (s *Store) saveLocked(cfg Config) error {
	normalizeConfig(&cfg)
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return err
	}
	// 0600: the file holds the pre-auth key.
	if err := storage.AtomicWritePerm(s.path, data, 0600); err != nil {
		return err
	}
	stored := copyConfig(cfg)
	s.cfg = &stored
	return nil
}

// copyConfig gives the returned value its own backing array so a caller
// mutating AdvertiseRoutes cannot race readers of the cache.
func copyConfig(cfg Config) Config {
	cfg.AdvertiseRoutes = append([]string(nil), cfg.AdvertiseRoutes...)
	return cfg
}

// normalizeConfig repairs anything a hand-edited or older file can carry.
func normalizeConfig(cfg *Config) {
	if cfg.EgressMode != EgressWAN && cfg.EgressMode != EgressTunnel {
		cfg.EgressMode = EgressWAN
	}
	if cfg.TunnelFallback != "drop" && cfg.TunnelFallback != "bypass" {
		cfg.TunnelFallback = "drop"
	}
	if cfg.AdvertiseRoutes == nil {
		cfg.AdvertiseRoutes = []string{}
	}
	if cfg.EgressMode == EgressWAN {
		cfg.EgressTunnelID = ""
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tailscale/... -v`
Expected: PASS, four tests.

- [ ] **Step 5: Commit**

```bash
git add internal/tailscale/types.go internal/tailscale/store.go internal/tailscale/store_test.go
git commit -m "feat(tailscale): config types and persisted store"
```

---

### Task 2: Pinned binary installer

**Files:**
- Create: `internal/tailscale/install.go`
- Test: `internal/tailscale/install_test.go`

**Interfaces:**
- Consumes: `childproc.Install`, `childproc.Downloader` (`internal/childproc/install.go:22`).
- Produces: `var EmbeddedBinaries map[string]BinarySpec`, `PinnedVersion`, `type Installer`, `NewInstaller(arch, dir string, d childproc.Downloader) *Installer`, `(*Installer).Ensure(ctx) error`, `(*Installer).Status() InstallStatus`, `(*Installer).DaemonPath() string`, `(*Installer).CLIPath() string`.

- [ ] **Step 1: Write the failing test**

```go
package tailscale

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

type fakeDownloader struct {
	payload  []byte
	lastURL  string
	lastMax  int64
	callCount int
}

func (f *fakeDownloader) DownloadFile(_ context.Context, url, destPath string, maxBytes int64) error {
	f.callCount++
	f.lastURL = url
	f.lastMax = maxBytes
	return os.WriteFile(destPath, f.payload, 0644)
}

func TestEmbeddedBinaries_CoversEveryArchKey(t *testing.T) {
	for _, arch := range []string{"mipsel-3.4", "mips-3.4", "aarch64-3.10"} {
		spec, ok := EmbeddedBinaries[arch]
		if !ok {
			t.Errorf("no spec for %s", arch)
			continue
		}
		if spec.SHA256 == "" || spec.URL == "" || spec.Size == 0 {
			t.Errorf("%s: incomplete spec %+v", arch, spec)
		}
		if spec.Version != PinnedVersion {
			t.Errorf("%s: version %q != pinned %q", arch, spec.Version, PinnedVersion)
		}
	}
}

func TestInstaller_UnknownArchIsRejected(t *testing.T) {
	i := NewInstaller("sparc-9", t.TempDir(), &fakeDownloader{})
	if err := i.Ensure(context.Background()); err == nil {
		t.Fatal("expected error for unsupported arch")
	}
}

func TestInstaller_SkipsDownloadWhenVersionRecordMatches(t *testing.T) {
	dir := t.TempDir()
	d := &fakeDownloader{payload: []byte("binary")}
	i := NewInstaller("aarch64-3.10", dir, d)
	// Pretend the pinned version is already installed and on disk.
	if err := os.WriteFile(i.DaemonPath(), []byte("binary"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := i.writeInstalledVersion(PinnedVersion); err != nil {
		t.Fatal(err)
	}
	st := i.Status()
	if !st.Installed || st.UpdateAvailable {
		t.Fatalf("Status = %+v, want installed and up to date", st)
	}
	if d.callCount != 0 {
		t.Errorf("downloader called %d times, want 0", d.callCount)
	}
}

func TestInstaller_StatusReportsUpdateWhenBinaryMissing(t *testing.T) {
	dir := t.TempDir()
	i := NewInstaller("aarch64-3.10", dir, &fakeDownloader{})
	if err := i.writeInstalledVersion(PinnedVersion); err != nil {
		t.Fatal(err)
	}
	if st := i.Status(); st.Installed {
		t.Errorf("Status.Installed = true with no binary on disk: %+v", st)
	}
	if _, err := os.Stat(filepath.Join(dir, "tailscaled")); !os.IsNotExist(err) {
		t.Errorf("unexpected binary present: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tailscale/... -run "TestEmbedded|TestInstaller" -v`
Expected: FAIL — undefined `EmbeddedBinaries`, `NewInstaller`.

- [ ] **Step 3: Write minimal implementation**

`internal/tailscale/install.go` — modeled on `internal/wdtt/install.go`:

```go
package tailscale

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/hoaxisr/awg-manager/internal/childproc"
)

// PinnedVersion is the upstream tailscale release this build is verified
// against. Bumping it requires regenerating every SHA256 below.
const PinnedVersion = "1.90.8"

// releaseBase — mirror on repo.hoaxisr.ru (parity with wdtt/freeturn):
// upstream static builds, repacked, so the router never needs HTTPS to a
// third-party CDN.
const releaseBase = "http://repo.hoaxisr.ru/tailscale/" + PinnedVersion + "/"

// BinarySpec is one architecture's pinned download metadata.
type BinarySpec struct {
	Version    string
	URL        string // tailscaled (combined binary; the CLI is a symlink to it)
	SHA256     string
	Size       int64
	CLIURL     string // separate tailscale CLI binary
	CLISHA256  string
	CLISize    int64
}

// EmbeddedBinaries maps the awg-manager build arch to pinned assets.
//
// PLACEHOLDER VALUES: the SHA256/Size fields below MUST be replaced with the
// real checksums produced by scripts/regen-embedded-tailscale.sh in Step 5 of
// this task. childproc.Install fails closed on an empty SHA256, so a forgotten
// value is caught at runtime, but a WRONG value is not — regenerate, do not
// hand-edit.
var EmbeddedBinaries = map[string]BinarySpec{
	"mipsel-3.4": {
		Version: PinnedVersion,
		URL:     releaseBase + "tailscaled-linux-mipsle",
		CLIURL:  releaseBase + "tailscale-linux-mipsle",
	},
	"mips-3.4": {
		Version: PinnedVersion,
		URL:     releaseBase + "tailscaled-linux-mips",
		CLIURL:  releaseBase + "tailscale-linux-mips",
	},
	"aarch64-3.10": {
		Version: PinnedVersion,
		URL:     releaseBase + "tailscaled-linux-arm64",
		CLIURL:  releaseBase + "tailscale-linux-arm64",
	},
}

// InstallStatus is what the UI banner renders from.
type InstallStatus struct {
	Installed        bool   `json:"installed"`
	UpdateAvailable  bool   `json:"updateAvailable"`
	InstalledVersion string `json:"installedVersion"`
	RequiredVersion  string `json:"requiredVersion"`
	ArchSupported    bool   `json:"archSupported"`
}

type Installer struct {
	arch       string
	dir        string
	downloader childproc.Downloader
}

func NewInstaller(arch, dir string, d childproc.Downloader) *Installer {
	return &Installer{arch: arch, dir: dir, downloader: d}
}

func (i *Installer) DaemonPath() string { return filepath.Join(i.dir, "tailscaled") }
func (i *Installer) CLIPath() string    { return filepath.Join(i.dir, "tailscale") }
func (i *Installer) versionPath() string {
	return filepath.Join(i.dir, "installed-version.json")
}

// Ensure downloads and activates both binaries when they are missing or when
// the on-disk version record does not match PinnedVersion.
func (i *Installer) Ensure(ctx context.Context) error {
	spec, ok := EmbeddedBinaries[i.arch]
	if !ok {
		return fmt.Errorf("архитектура %q не поддерживается для tailscale", i.arch)
	}
	if st := i.Status(); st.Installed && !st.UpdateAvailable {
		return nil
	}
	if err := os.MkdirAll(i.dir, 0755); err != nil {
		return err
	}
	if err := childproc.Install(ctx, i.downloader, i.DaemonPath(), spec.URL, spec.SHA256, spec.Size); err != nil {
		return fmt.Errorf("установка tailscaled: %w", err)
	}
	if err := childproc.Install(ctx, i.downloader, i.CLIPath(), spec.CLIURL, spec.CLISHA256, spec.CLISize); err != nil {
		return fmt.Errorf("установка tailscale CLI: %w", err)
	}
	return i.writeInstalledVersion(spec.Version)
}

func (i *Installer) Status() InstallStatus {
	_, archOK := EmbeddedBinaries[i.arch]
	st := InstallStatus{
		RequiredVersion:  PinnedVersion,
		InstalledVersion: i.readInstalledVersion(),
		ArchSupported:    archOK,
	}
	st.Installed = archOK && binaryPresent(i.DaemonPath()) && binaryPresent(i.CLIPath())
	st.UpdateAvailable = archOK && (!st.Installed || st.InstalledVersion != PinnedVersion)
	return st
}

type installedVersionRecord struct {
	Version     string    `json:"version"`
	InstalledAt time.Time `json:"installedAt"`
}

func (i *Installer) readInstalledVersion() string {
	data, err := os.ReadFile(i.versionPath())
	if err != nil {
		return ""
	}
	var rec installedVersionRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return ""
	}
	return rec.Version
}

func (i *Installer) writeInstalledVersion(version string) error {
	data, err := json.MarshalIndent(installedVersionRecord{
		Version:     version,
		InstalledAt: time.Now().UTC(),
	}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(i.dir, 0755); err != nil {
		return err
	}
	tmp := i.versionPath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, i.versionPath())
}

func binaryPresent(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Size() > 0
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tailscale/... -v`
Expected: PASS. `TestEmbeddedBinaries_CoversEveryArchKey` FAILS until Step 5 fills in checksums — that is the intended red state; do not weaken the test.

- [ ] **Step 5: Generate the real pinned checksums**

Create `scripts/regen-embedded-tailscale.sh` following `scripts/` conventions: fetch the three upstream `tailscale_<version>_<arch>.tgz` archives, extract `tailscaled` and `tailscale`, upload to `repo.hoaxisr.ru/tailscale/<version>/`, then print the Go literal with real `SHA256`/`Size` for each arch. Paste its output over the placeholder map.

Run: `sh scripts/regen-embedded-tailscale.sh` then re-run the test.
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/tailscale/install.go internal/tailscale/install_test.go scripts/regen-embedded-tailscale.sh
git commit -m "feat(tailscale): pinned binary installer"
```

---

### Task 3: Preflight checks

**Files:**
- Create: `internal/tailscale/preflight.go`
- Test: `internal/tailscale/preflight_test.go`

**Interfaces:**
- Consumes: `Installer.Status()` from Task 2.
- Produces: `type PreflightResult struct { OK bool; Blockers []Blocker }`, `type Blocker struct { Code, Message string }`, `func Preflight(ctx context.Context, deps PreflightDeps) PreflightResult`, `type PreflightDeps` with injectable probes so the suite runs on darwin.

- [ ] **Step 1: Write the failing test**

```go
package tailscale

import (
	"context"
	"testing"
)

func newTestPreflightDeps() PreflightDeps {
	return PreflightDeps{
		TunPresent:      func() bool { return true },
		FreeBytes:       func() (int64, bool) { return 512 << 20, true },
		RequiredBytes:   func() int64 { return 40 << 20 },
		ForeignDaemonPID: func(context.Context) int { return 0 },
		PortInUse:       func(context.Context, int) bool { return false },
		FreeOpkgTunIndex: func(context.Context) (int, bool) { return 18, true },
		IsOS5:           func() bool { return true },
	}
}

func TestPreflight_AllGreen(t *testing.T) {
	res := Preflight(context.Background(), newTestPreflightDeps())
	if !res.OK || len(res.Blockers) != 0 {
		t.Fatalf("want clean preflight, got %+v", res)
	}
}

func TestPreflight_MissingTunBlocks(t *testing.T) {
	d := newTestPreflightDeps()
	d.TunPresent = func() bool { return false }
	res := Preflight(context.Background(), d)
	if res.OK {
		t.Fatal("expected preflight to fail")
	}
	if res.Blockers[0].Code != BlockerNoTun {
		t.Errorf("Blockers[0].Code = %q, want %q", res.Blockers[0].Code, BlockerNoTun)
	}
}

func TestPreflight_InsufficientSpaceBlocks(t *testing.T) {
	d := newTestPreflightDeps()
	d.FreeBytes = func() (int64, bool) { return 10 << 20, true }
	res := Preflight(context.Background(), d)
	if res.OK {
		t.Fatal("expected preflight to fail")
	}
	if res.Blockers[0].Code != BlockerNoSpace {
		t.Errorf("Blockers[0].Code = %q, want %q", res.Blockers[0].Code, BlockerNoSpace)
	}
}

func TestPreflight_UnknownFreeSpaceDoesNotBlock(t *testing.T) {
	d := newTestPreflightDeps()
	d.FreeBytes = func() (int64, bool) { return 0, false }
	if res := Preflight(context.Background(), d); !res.OK {
		t.Fatalf("unknown free space must not block, got %+v", res)
	}
}

func TestPreflight_ForeignDaemonAndBusyPortBothReported(t *testing.T) {
	d := newTestPreflightDeps()
	d.ForeignDaemonPID = func(context.Context) int { return 4242 }
	d.PortInUse = func(context.Context, int) bool { return true }
	res := Preflight(context.Background(), d)
	codes := map[string]bool{}
	for _, b := range res.Blockers {
		codes[b.Code] = true
	}
	if !codes[BlockerForeignDaemon] || !codes[BlockerPortBusy] {
		t.Errorf("want both blockers, got %+v", res.Blockers)
	}
}

func TestPreflight_NoFreeSlotOnlyBlocksOnOS5(t *testing.T) {
	d := newTestPreflightDeps()
	d.FreeOpkgTunIndex = func(context.Context) (int, bool) { return 0, false }
	if res := Preflight(context.Background(), d); res.OK {
		t.Error("OS5 with no free slot must block")
	}
	d.IsOS5 = func() bool { return false }
	if res := Preflight(context.Background(), d); !res.OK {
		t.Errorf("OS4 has no slots to allocate, must not block: %+v", res)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tailscale/... -run TestPreflight -v`
Expected: FAIL — undefined `Preflight`, `PreflightDeps`.

- [ ] **Step 3: Write minimal implementation**

```go
package tailscale

import (
	"context"
	"fmt"
	"os"
)

// Blocker codes. The UI maps each to an actionable message; the Message
// field carries the fallback text.
const (
	BlockerNoTun         = "no_tun"
	BlockerNoSpace       = "no_space"
	BlockerForeignDaemon = "foreign_daemon"
	BlockerPortBusy      = "port_busy"
	BlockerNoSlot        = "no_opkgtun_slot"
)

// DefaultPort is tailscaled's WireGuard listen port.
const DefaultPort = 41641

type Blocker struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type PreflightResult struct {
	OK       bool      `json:"ok"`
	Blockers []Blocker `json:"blockers"`
}

// PreflightDeps injects every environment probe so the suite runs on darwin.
type PreflightDeps struct {
	TunPresent       func() bool
	FreeBytes        func() (int64, bool)
	RequiredBytes    func() int64
	ForeignDaemonPID func(context.Context) int
	PortInUse        func(context.Context, int) bool
	FreeOpkgTunIndex func(context.Context) (int, bool)
	IsOS5            func() bool
}

// Preflight collects every reason enabling tailscale would fail. It reports
// ALL blockers rather than the first — a user with two problems should see
// two, not discover the second after fixing the first.
func Preflight(ctx context.Context, d PreflightDeps) PreflightResult {
	var blockers []Blocker

	if d.TunPresent != nil && !d.TunPresent() {
		blockers = append(blockers, Blocker{
			Code:    BlockerNoTun,
			Message: "TUN-устройство /dev/net/tun недоступно — exit node и маршрутизация подсетей без него невозможны",
		})
	}
	if d.FreeBytes != nil && d.RequiredBytes != nil {
		// Unknown free space is NOT a blocker: statfs is unavailable on some
		// targets and childproc.Install still hard-caps the transfer.
		if free, ok := d.FreeBytes(); ok && free < d.RequiredBytes() {
			blockers = append(blockers, Blocker{
				Code:    BlockerNoSpace,
				Message: fmt.Sprintf("недостаточно места: свободно %d МБ, требуется %d МБ", free>>20, d.RequiredBytes()>>20),
			})
		}
	}
	if d.ForeignDaemonPID != nil {
		if pid := d.ForeignDaemonPID(ctx); pid > 0 {
			blockers = append(blockers, Blocker{
				Code:    BlockerForeignDaemon,
				Message: fmt.Sprintf("уже запущен сторонний tailscaled (pid %d) — остановите его", pid),
			})
		}
	}
	if d.PortInUse != nil && d.PortInUse(ctx, DefaultPort) {
		blockers = append(blockers, Blocker{
			Code:    BlockerPortBusy,
			Message: fmt.Sprintf("UDP-порт %d занят другим процессом", DefaultPort),
		})
	}
	// OS 4.x has no NDMS interface slots — the daemon uses a plain tailscale0.
	if d.IsOS5 != nil && d.IsOS5() && d.FreeOpkgTunIndex != nil {
		if _, ok := d.FreeOpkgTunIndex(ctx); !ok {
			blockers = append(blockers, Blocker{
				Code:    BlockerNoSlot,
				Message: "нет свободного слота OpkgTun — освободите один из туннелей",
			})
		}
	}

	return PreflightResult{OK: len(blockers) == 0, Blockers: blockers}
}

// TunDevicePresent is the production TunPresent probe. sing-box's fakeip-tun
// already proves the device exists on Keenetic (see the design spec §3); this
// is cheap insurance, not a discovery mechanism.
func TunDevicePresent() bool {
	info, err := os.Stat("/dev/net/tun")
	return err == nil && info.Mode()&os.ModeDevice != 0
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tailscale/... -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tailscale/preflight.go internal/tailscale/preflight_test.go
git commit -m "feat(tailscale): preflight gate with actionable blockers"
```

---

### Task 4: CLI argv builder, login-URL capture, secret scrubbing

**Files:**
- Create: `internal/tailscale/cli.go`
- Create: `internal/tailscale/scrub.go`
- Test: `internal/tailscale/cli_test.go`
- Test: `internal/tailscale/scrub_test.go`

**Interfaces:**
- Consumes: `Config` (Task 1), `Installer.CLIPath()` (Task 2).
- Produces: `func UpArgs(cfg Config, socket string) []string`, `func DownArgs(socket string) []string`, `func LogoutArgs(socket string) []string`, `func ExtractLoginURL(output string) string`, `func Scrub(s string) string`.

- [ ] **Step 1: Write the failing test**

```go
package tailscale

import (
	"strings"
	"testing"
)

func joined(args []string) string { return strings.Join(args, " ") }

func TestUpArgs_AlwaysDisablesAcceptDNS(t *testing.T) {
	got := joined(UpArgs(DefaultConfig(), "/run/ts.sock"))
	if !strings.Contains(got, "--accept-dns=false") {
		t.Errorf("accept-dns must always be disabled: %s", got)
	}
}

func TestUpArgs_OmitsExitNodeWhenDisabled(t *testing.T) {
	got := joined(UpArgs(DefaultConfig(), "/run/ts.sock"))
	if strings.Contains(got, "--advertise-exit-node") {
		t.Errorf("exit node must be opt-in: %s", got)
	}
}

func TestUpArgs_EmitsEveryEnabledFlag(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ExitNode = true
	cfg.AdvertiseRoutes = []string{"192.168.1.0/24", "10.10.0.0/16"}
	cfg.Hostname = "keenetic"
	cfg.LoginServer = "https://hs.example.org"
	cfg.AuthKey = "tskey-auth-abc123"
	got := joined(UpArgs(cfg, "/run/ts.sock"))
	for _, want := range []string{
		"--socket=/run/ts.sock",
		"--advertise-exit-node",
		"--advertise-routes=192.168.1.0/24,10.10.0.0/16",
		"--snat-subnet-routes=true",
		"--hostname=keenetic",
		"--login-server=https://hs.example.org",
		"--authkey=tskey-auth-abc123",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %s", want, got)
		}
	}
}

func TestUpArgs_OmitsEmptyAdvertiseRoutes(t *testing.T) {
	if got := joined(UpArgs(DefaultConfig(), "/run/ts.sock")); strings.Contains(got, "--advertise-routes") {
		t.Errorf("empty route list must not emit the flag: %s", got)
	}
}

func TestExtractLoginURL(t *testing.T) {
	out := "\nTo authenticate, visit:\n\n\thttps://login.tailscale.com/a/1a2b3c4d5e\n\n"
	if got := ExtractLoginURL(out); got != "https://login.tailscale.com/a/1a2b3c4d5e" {
		t.Errorf("ExtractLoginURL = %q", got)
	}
}

func TestExtractLoginURL_CustomControlServer(t *testing.T) {
	out := "To authenticate, visit:\n\thttps://hs.example.org/register/nodekey:abcdef\n"
	if got := ExtractLoginURL(out); got != "https://hs.example.org/register/nodekey:abcdef" {
		t.Errorf("ExtractLoginURL = %q", got)
	}
}

func TestExtractLoginURL_NoURL(t *testing.T) {
	if got := ExtractLoginURL("Success."); got != "" {
		t.Errorf("want empty, got %q", got)
	}
}
```

`scrub_test.go`:

```go
package tailscale

import (
	"strings"
	"testing"
)

func TestScrub_RedactsAuthKeyInArgvAndPlainText(t *testing.T) {
	for _, in := range []string{
		"tailscale up --authkey=tskey-auth-kX9dLm2p3q --hostname=keenetic",
		"backend error: invalid key tskey-auth-kX9dLm2p3q",
		"--authkey tskey-auth-kX9dLm2p3q",
	} {
		got := Scrub(in)
		if strings.Contains(got, "kX9dLm2p3q") {
			t.Errorf("secret survived scrubbing: %q -> %q", in, got)
		}
		if !strings.Contains(got, "***") {
			t.Errorf("no redaction marker: %q -> %q", in, got)
		}
	}
}

func TestScrub_RedactsClientAndAPIKeyPrefixes(t *testing.T) {
	for _, in := range []string{"tskey-client-abc123def", "tskey-api-zzz999"} {
		if got := Scrub(in); strings.Contains(got, "abc123def") || strings.Contains(got, "zzz999") {
			t.Errorf("secret survived: %q -> %q", in, got)
		}
	}
}

func TestScrub_LeavesOrdinaryTextAlone(t *testing.T) {
	in := "peer keenetic 100.64.0.3 online direct"
	if got := Scrub(in); got != in {
		t.Errorf("Scrub mangled clean text: %q -> %q", in, got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tailscale/... -run "TestUpArgs|TestExtract|TestScrub" -v`
Expected: FAIL — undefined `UpArgs`, `ExtractLoginURL`, `Scrub`.

- [ ] **Step 3: Write minimal implementation**

`internal/tailscale/cli.go`:

```go
package tailscale

import (
	"regexp"
	"strings"
)

// UpArgs builds the argv for `tailscale up` from persisted desired state.
// Config is the single source of truth — nothing here reads live daemon state,
// so "what the UI shows" and "what the daemon was told" cannot diverge.
func UpArgs(cfg Config, socket string) []string {
	args := []string{
		"--socket=" + socket,
		"up",
		// Never negotiable: tailscaled would otherwise rewrite
		// /etc/resolv.conf, which NDMS owns and rewrites back.
		"--accept-dns=false",
		"--snat-subnet-routes=true",
	}
	if len(cfg.AdvertiseRoutes) > 0 {
		args = append(args, "--advertise-routes="+strings.Join(cfg.AdvertiseRoutes, ","))
	}
	if cfg.ExitNode {
		args = append(args, "--advertise-exit-node")
	}
	if cfg.Hostname != "" {
		args = append(args, "--hostname="+cfg.Hostname)
	}
	if cfg.LoginServer != "" {
		args = append(args, "--login-server="+cfg.LoginServer)
	}
	if cfg.AuthKey != "" {
		args = append(args, "--authkey="+cfg.AuthKey)
	}
	return args
}

func DownArgs(socket string) []string   { return []string{"--socket=" + socket, "down"} }
func LogoutArgs(socket string) []string { return []string{"--socket=" + socket, "logout"} }

// loginURLRe matches both the official control server and a Headscale
// register URL, which is why it is not anchored to login.tailscale.com.
var loginURLRe = regexp.MustCompile(`https://[^\s]+/(?:a|register)/[^\s]+`)

// ExtractLoginURL pulls the interactive auth URL out of `tailscale up` output.
// Returns "" when the node authenticated non-interactively (pre-auth key).
func ExtractLoginURL(output string) string {
	return loginURLRe.FindString(output)
}
```

`internal/tailscale/scrub.go`:

```go
package tailscale

import "regexp"

// tsKeyRe matches every tailscale secret shape (auth, client, api keys).
// Applied to every log line, process output tail and error string that can
// reach the UI — an auth key in a log is an auth key in a bug report.
var tsKeyRe = regexp.MustCompile(`tskey-(?:auth|client|api)-[A-Za-z0-9]+`)

// Scrub replaces tailscale secrets with a redaction marker.
func Scrub(s string) string {
	return tsKeyRe.ReplaceAllString(s, "tskey-***")
}

// ScrubArgs applies Scrub to a copy of argv, for logging a command line.
func ScrubArgs(args []string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = Scrub(a)
	}
	return out
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tailscale/... -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tailscale/cli.go internal/tailscale/scrub.go internal/tailscale/cli_test.go internal/tailscale/scrub_test.go
git commit -m "feat(tailscale): CLI argv builder and secret scrubbing"
```

---

### Task 5: LocalAPI status client

**Files:**
- Create: `internal/tailscale/localapi.go`
- Test: `internal/tailscale/localapi_test.go`
- Test fixture: `internal/tailscale/testdata/status_running.json`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `type LocalAPI`, `NewLocalAPI(socketPath string) *LocalAPI`, `(*LocalAPI).Status(ctx) (Status, error)`, `type Status`, `type Peer`, `func parseStatus([]byte) (Status, error)`.

- [ ] **Step 1: Write the failing test**

```go
package tailscale

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestParseStatus_MapsBackendStateSelfAndPeers(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "status_running.json"))
	if err != nil {
		t.Fatal(err)
	}
	st, err := parseStatus(raw)
	if err != nil {
		t.Fatalf("parseStatus: %v", err)
	}
	if st.BackendState != "Running" {
		t.Errorf("BackendState = %q", st.BackendState)
	}
	if st.SelfIP != "100.64.0.1" || st.SelfHostname != "keenetic" {
		t.Errorf("self = %q/%q", st.SelfIP, st.SelfHostname)
	}
	if len(st.Peers) != 2 {
		t.Fatalf("len(Peers) = %d, want 2", len(st.Peers))
	}
	laptop := st.Peers[0]
	if laptop.Hostname != "laptop" || !laptop.Online || laptop.Relay != "" || laptop.IP != "100.64.0.2" {
		t.Errorf("peer[0] = %+v", laptop)
	}
	phone := st.Peers[1]
	if phone.Online || phone.Relay != "fra" {
		t.Errorf("peer[1] = %+v", phone)
	}
}

func TestParseStatus_ReportsAdvertisedButUnapprovedRoutes(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "status_running.json"))
	if err != nil {
		t.Fatal(err)
	}
	st, _ := parseStatus(raw)
	if len(st.AdvertisedRoutes) != 2 {
		t.Fatalf("AdvertisedRoutes = %v", st.AdvertisedRoutes)
	}
	if len(st.ApprovedRoutes) != 1 {
		t.Fatalf("ApprovedRoutes = %v", st.ApprovedRoutes)
	}
	if !st.RoutesPendingApproval() {
		t.Error("RoutesPendingApproval() = false with 2 advertised / 1 approved")
	}
}

func TestParseStatus_NeedsLoginHasNoSelfIP(t *testing.T) {
	st, err := parseStatus([]byte(`{"BackendState":"NeedsLogin","Self":null,"Peer":{}}`))
	if err != nil {
		t.Fatalf("parseStatus: %v", err)
	}
	if st.BackendState != "NeedsLogin" || st.SelfIP != "" {
		t.Errorf("st = %+v", st)
	}
}

func TestLocalAPI_StatusOverUnixSocket(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "ts.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Skipf("unix sockets unavailable: %v", err)
	}
	defer ln.Close()
	mux := http.NewServeMux()
	mux.HandleFunc("/localapi/v0/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "local-tailscaled.sock" {
			t.Errorf("Host = %q, want local-tailscaled.sock", r.Host)
		}
		http.ServeFile(w, r, filepath.Join("testdata", "status_running.json"))
	})
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()

	st, err := NewLocalAPI(sock).Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.BackendState != "Running" {
		t.Errorf("BackendState = %q", st.BackendState)
	}
}

func TestLocalAPI_StatusFailsWhenSocketAbsent(t *testing.T) {
	_, err := NewLocalAPI(filepath.Join(t.TempDir(), "missing.sock")).Status(context.Background())
	if err == nil {
		t.Fatal("expected error for missing socket")
	}
}
```

- [ ] **Step 2: Create the fixture**

`internal/tailscale/testdata/status_running.json`:

```json
{
  "BackendState": "Running",
  "Self": {
    "HostName": "keenetic",
    "TailscaleIPs": ["100.64.0.1"],
    "Online": true,
    "PrimaryRoutes": ["192.168.1.0/24"],
    "AllowedIPs": ["100.64.0.1/32", "192.168.1.0/24"]
  },
  "Peer": {
    "nodekey:aaa": {
      "HostName": "laptop",
      "TailscaleIPs": ["100.64.0.2"],
      "Online": true,
      "Relay": "",
      "RxBytes": 1024,
      "TxBytes": 2048,
      "LastHandshake": "2026-08-03T10:00:00Z"
    },
    "nodekey:bbb": {
      "HostName": "phone",
      "TailscaleIPs": ["100.64.0.3"],
      "Online": false,
      "Relay": "fra",
      "RxBytes": 0,
      "TxBytes": 0,
      "LastHandshake": "0001-01-01T00:00:00Z"
    }
  },
  "AdvertisedRoutes": ["192.168.1.0/24", "10.10.0.0/16"]
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/tailscale/... -run "TestParseStatus|TestLocalAPI" -v`
Expected: FAIL — undefined `parseStatus`, `NewLocalAPI`.

- [ ] **Step 4: Write minimal implementation**

```go
package tailscale

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"time"
)

// Peer is one tailnet node as shown in the UI.
type Peer struct {
	Hostname      string    `json:"hostname"`
	IP            string    `json:"ip"`
	Online        bool      `json:"online"`
	Relay         string    `json:"relay"` // "" = direct connection, else the DERP region
	RxBytes       int64     `json:"rxBytes"`
	TxBytes       int64     `json:"txBytes"`
	LastHandshake time.Time `json:"lastHandshake"`
}

// Status is the LocalAPI view the UI renders.
type Status struct {
	BackendState     string   `json:"backendState"` // NoState|NeedsLogin|Starting|Running|Stopped
	SelfHostname     string   `json:"selfHostname"`
	SelfIP           string   `json:"selfIp"`
	Peers            []Peer   `json:"peers"`
	AdvertisedRoutes []string `json:"advertisedRoutes"`
	ApprovedRoutes   []string `json:"approvedRoutes"`
}

// RoutesPendingApproval reports whether the node advertises a route the
// control plane has not approved. Without this the UI would look healthy
// while no tailnet client can actually reach the LAN.
func (s Status) RoutesPendingApproval() bool {
	approved := make(map[string]bool, len(s.ApprovedRoutes))
	for _, r := range s.ApprovedRoutes {
		approved[r] = true
	}
	for _, r := range s.AdvertisedRoutes {
		if !approved[r] {
			return true
		}
	}
	return false
}

// rawStatus mirrors only the LocalAPI fields we consume. The endpoint is an
// unstable internal API, so every field is optional and a shape change
// degrades the view rather than erroring the whole poll.
type rawStatus struct {
	BackendState string `json:"BackendState"`
	Self         *struct {
		HostName      string   `json:"HostName"`
		TailscaleIPs  []string `json:"TailscaleIPs"`
		PrimaryRoutes []string `json:"PrimaryRoutes"`
	} `json:"Self"`
	Peer map[string]struct {
		HostName      string    `json:"HostName"`
		TailscaleIPs  []string  `json:"TailscaleIPs"`
		Online        bool      `json:"Online"`
		Relay         string    `json:"Relay"`
		RxBytes       int64     `json:"RxBytes"`
		TxBytes       int64     `json:"TxBytes"`
		LastHandshake time.Time `json:"LastHandshake"`
	} `json:"Peer"`
	AdvertisedRoutes []string `json:"AdvertisedRoutes"`
}

func parseStatus(data []byte) (Status, error) {
	var raw rawStatus
	if err := json.Unmarshal(data, &raw); err != nil {
		return Status{}, fmt.Errorf("разбор status: %w", err)
	}
	st := Status{
		BackendState:     raw.BackendState,
		AdvertisedRoutes: raw.AdvertisedRoutes,
	}
	if raw.Self != nil {
		st.SelfHostname = raw.Self.HostName
		if len(raw.Self.TailscaleIPs) > 0 {
			st.SelfIP = raw.Self.TailscaleIPs[0]
		}
		// PrimaryRoutes is what the control plane actually serves for this
		// node — i.e. the approved subset of AdvertisedRoutes.
		st.ApprovedRoutes = raw.Self.PrimaryRoutes
	}
	for _, p := range raw.Peer {
		peer := Peer{
			Hostname:      p.HostName,
			Online:        p.Online,
			Relay:         p.Relay,
			RxBytes:       p.RxBytes,
			TxBytes:       p.TxBytes,
			LastHandshake: p.LastHandshake,
		}
		if len(p.TailscaleIPs) > 0 {
			peer.IP = p.TailscaleIPs[0]
		}
		st.Peers = append(st.Peers, peer)
	}
	// Map iteration is random; sort so the UI list does not reshuffle on
	// every 5s poll.
	sort.Slice(st.Peers, func(i, j int) bool { return st.Peers[i].Hostname < st.Peers[j].Hostname })
	return st, nil
}

// LocalAPI is a minimal read-only client for tailscaled's unix socket.
type LocalAPI struct {
	socketPath string
	client     *http.Client
}

func NewLocalAPI(socketPath string) *LocalAPI {
	return &LocalAPI{
		socketPath: socketPath,
		client: &http.Client{
			Timeout: 5 * time.Second,
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
				},
			},
		},
	}
}

func (l *LocalAPI) Status(ctx context.Context) (Status, error) {
	// The host is ignored by the unix dialer but tailscaled validates it.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"http://local-tailscaled.sock/localapi/v0/status", nil)
	if err != nil {
		return Status{}, err
	}
	resp, err := l.client.Do(req)
	if err != nil {
		return Status{}, fmt.Errorf("localapi: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Status{}, fmt.Errorf("localapi: статус %d", resp.StatusCode)
	}
	data, err := readAllLimited(resp.Body, 8<<20)
	if err != nil {
		return Status{}, err
	}
	return parseStatus(data)
}
```

Add the bounded reader in the same file (a large tailnet must not be able to exhaust router memory):

```go
func readAllLimited(r io.Reader, max int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, max))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) == max {
		return nil, fmt.Errorf("localapi: ответ больше %d байт", max)
	}
	return data, nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/tailscale/... -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/tailscale/localapi.go internal/tailscale/localapi_test.go internal/tailscale/testdata
git commit -m "feat(tailscale): LocalAPI status client"
```

---

### Task 6: Daemon process supervision

**Files:**
- Create: `internal/tailscale/process.go`
- Test: `internal/tailscale/process_test.go`

**Interfaces:**
- Consumes: `childproc.RingBuffer`, `childproc.SetProcessGroup`, `childproc.Terminate`, `childproc.IsAlive` (`internal/childproc/platform_linux.go`), `Scrub` (Task 4).
- Produces: `type process`, `newProcess(binary, runtimeDir string) *process`, `(*process).Start(args []string) error`, `(*process).Stop() error`, `(*process).IsRunning() (bool, int)`, `(*process).LogTail(n int) string`, field `startCmd func(bin string, args ...string) *exec.Cmd` as the test seam.

- [ ] **Step 1: Write the failing test**

```go
package tailscale

import (
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestProcess_StartRecordsPIDAndDrainsLog(t *testing.T) {
	p := newProcess("/bin/sh", t.TempDir())
	p.startCmd = func(_ string, _ ...string) *exec.Cmd {
		return exec.Command("/bin/sh", "-c", "echo hello from daemon; sleep 5")
	}
	if err := p.Start([]string{"--ignored"}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer p.Stop()

	running, pid := p.IsRunning()
	if !running || pid <= 0 {
		t.Fatalf("IsRunning = %v/%d", running, pid)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(p.LogTail(10), "hello from daemon") {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Errorf("log line never drained: %q", p.LogTail(10))
}

func TestProcess_LogTailIsScrubbed(t *testing.T) {
	p := newProcess("/bin/sh", t.TempDir())
	p.startCmd = func(_ string, _ ...string) *exec.Cmd {
		return exec.Command("/bin/sh", "-c", "echo using tskey-auth-SECRET123; sleep 5")
	}
	if err := p.Start(nil); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer p.Stop()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		tail := p.LogTail(10)
		if strings.Contains(tail, "tskey-") {
			if strings.Contains(tail, "SECRET123") {
				t.Fatalf("secret survived into the log ring: %q", tail)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Errorf("nothing drained: %q", p.LogTail(10))
}

func TestProcess_StopIsIdempotent(t *testing.T) {
	p := newProcess("/bin/sh", t.TempDir())
	p.startCmd = func(_ string, _ ...string) *exec.Cmd {
		return exec.Command("/bin/sh", "-c", "sleep 5")
	}
	if err := p.Start(nil); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := p.Stop(); err != nil {
		t.Fatalf("first Stop: %v", err)
	}
	if err := p.Stop(); err != nil {
		t.Errorf("second Stop must be a no-op, got %v", err)
	}
	if running, _ := p.IsRunning(); running {
		t.Error("IsRunning after Stop")
	}
}

func TestProcess_StartTwiceDoesNotSpawnSecond(t *testing.T) {
	p := newProcess("/bin/sh", t.TempDir())
	spawns := 0
	p.startCmd = func(_ string, _ ...string) *exec.Cmd {
		spawns++
		return exec.Command("/bin/sh", "-c", "sleep 5")
	}
	if err := p.Start(nil); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer p.Stop()
	if err := p.Start(nil); err != nil {
		t.Fatalf("second Start: %v", err)
	}
	if spawns != 1 {
		t.Errorf("spawned %d times, want 1", spawns)
	}
}

func TestProcess_StartFailsWhenBinaryMissing(t *testing.T) {
	p := newProcess("/nonexistent/tailscaled", t.TempDir())
	if err := p.Start(nil); err == nil {
		t.Fatal("expected error for missing binary")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tailscale/... -run TestProcess -v`
Expected: FAIL — undefined `newProcess`.

- [ ] **Step 3: Write minimal implementation**

Model on `internal/wdtt/process.go`. Key points: `startMu` serialises Start so the watchdog tick and a UI Restart cannot both pass the `IsRunning` gate; every drained line goes through `Scrub` **before** it reaches the ring buffer, so a secret is never stored at all.

```go
package tailscale

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hoaxisr/awg-manager/internal/childproc"
)

const processLogMaxLines = 200

type process struct {
	binary  string
	pidPath string

	startMu sync.Mutex // serialises Start against a concurrent watchdog tick

	mu        sync.Mutex
	startedAt *time.Time
	lastErr   string
	logTail   *childproc.RingBuffer

	startCmd func(bin string, args ...string) *exec.Cmd
}

func newProcess(binary, runtimeDir string) *process {
	return &process{
		binary:  binary,
		pidPath: filepath.Join(runtimeDir, "tailscaled.pid"),
		logTail: childproc.NewRingBuffer(processLogMaxLines),
		startCmd: func(bin string, args ...string) *exec.Cmd {
			return exec.Command(bin, args...)
		},
	}
}

func (p *process) Start(args []string) error {
	p.startMu.Lock()
	defer p.startMu.Unlock()
	if running, _ := p.IsRunning(); running {
		return nil
	}
	if !binaryPresent(p.binary) {
		return fmt.Errorf("бинарь %s не найден — установите tailscale", p.binary)
	}
	if err := os.MkdirAll(filepath.Dir(p.pidPath), 0755); err != nil {
		return err
	}

	cmd := p.startCmd(p.binary, args...)
	childproc.SetProcessGroup(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("tailscaled stdout: %w", err)
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("запуск tailscaled: %w", err)
	}
	now := time.Now()
	p.mu.Lock()
	p.startedAt = &now
	p.lastErr = ""
	p.mu.Unlock()
	if err := os.WriteFile(p.pidPath, []byte(strconv.Itoa(cmd.Process.Pid)), 0644); err != nil {
		return err
	}
	go p.drain(stdout)
	go func() {
		err := cmd.Wait()
		p.mu.Lock()
		p.startedAt = nil
		if err != nil {
			p.lastErr = Scrub(err.Error())
		}
		p.mu.Unlock()
	}()
	return nil
}

// drain copies daemon output into the ring buffer. Scrub runs BEFORE the
// write: a secret that never enters the buffer cannot leak from it.
func (p *process) drain(r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 4096), 64*1024)
	for sc.Scan() {
		p.logTail.WriteLine(Scrub(sc.Text()))
	}
}

func (p *process) IsRunning() (bool, int) {
	data, err := os.ReadFile(p.pidPath)
	if err != nil {
		return false, 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return false, 0
	}
	if !childproc.IsAlive(pid) {
		return false, 0
	}
	return true, pid
}

// Stop terminates the daemon and removes the pid file. Calling it on an
// already-stopped process is a no-op, not an error — the watchdog and the UI
// both call it without coordinating.
func (p *process) Stop() error {
	running, pid := p.IsRunning()
	if !running {
		_ = os.Remove(p.pidPath)
		return nil
	}
	if err := childproc.Terminate(pid); err != nil {
		return err
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !childproc.IsAlive(pid) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if childproc.IsAlive(pid) {
		_ = childproc.Kill(pid)
	}
	_ = os.Remove(p.pidPath)
	p.mu.Lock()
	p.startedAt = nil
	p.mu.Unlock()
	return nil
}

func (p *process) LogTail(n int) string { return p.logTail.LastLines(n) }

func (p *process) LastError() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastErr
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tailscale/... -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tailscale/process.go internal/tailscale/process_test.go
git commit -m "feat(tailscale): supervised tailscaled process"
```

---

### Task 7: NDMS OpkgTun slot lifecycle (OS 5.x)

**Files:**
- Create: `internal/tailscale/ndms_iface.go`
- Test: `internal/tailscale/ndms_iface_test.go`

**Interfaces:**
- Consumes: `Config.OpkgTunIndex` (Task 1). Mirrors `internal/wdtt/ndms_iface.go`.
- Produces: `type NDMSInterfaces interface`, `type IndexLister interface`, `func opkgTunNDMSName(int) string`, `func opkgTunKernelName(int) string`, `func AllocateIndex(ctx, IndexLister, current int) (int, error)`, `func PrepareInterface(ctx, NDMSInterfaces, ndmsName string) error`, `func ActivateInterface(ctx, NDMSInterfaces, ndmsName string) error`, `func InterfaceName(cfg Config, isOS5 bool) string`.

- [ ] **Step 1: Write the failing test**

```go
package tailscale

import (
	"context"
	"testing"
)

type fakeNDMS struct {
	calls []string
	fail  map[string]error
}

func (f *fakeNDMS) CreateOpkgTunWithSecurityLevel(_ context.Context, name, _, level string) error {
	f.calls = append(f.calls, "create:"+name+":"+level)
	return f.fail["create"]
}
func (f *fakeNDMS) SetMTU(_ context.Context, name string, mtu int) error {
	f.calls = append(f.calls, "mtu:"+name)
	return f.fail["mtu"]
}
func (f *fakeNDMS) SetPermitAllACL(_ context.Context, name string) error {
	f.calls = append(f.calls, "acl:"+name)
	return f.fail["acl"]
}
func (f *fakeNDMS) InterfaceUp(_ context.Context, name string) error {
	f.calls = append(f.calls, "up:"+name)
	return f.fail["up"]
}
func (f *fakeNDMS) DeleteOpkgTun(_ context.Context, name string) error {
	f.calls = append(f.calls, "delete:"+name)
	return f.fail["delete"]
}

type fakeLister struct{ used map[int]bool }

func (f *fakeLister) LiveOpkgTunIndices(context.Context) (map[int]bool, error) {
	return f.used, nil
}

func TestNames(t *testing.T) {
	if got := opkgTunNDMSName(18); got != "OpkgTun18" {
		t.Errorf("ndms name = %q", got)
	}
	if got := opkgTunKernelName(18); got != "opkgtun18" {
		t.Errorf("kernel name = %q", got)
	}
}

func TestAllocateIndex_SkipsOccupied(t *testing.T) {
	l := &fakeLister{used: map[int]bool{17: true, 18: true}}
	idx, err := AllocateIndex(context.Background(), l, 0)
	if err != nil {
		t.Fatalf("AllocateIndex: %v", err)
	}
	if idx == 17 || idx == 18 {
		t.Errorf("allocated an occupied index %d", idx)
	}
}

func TestAllocateIndex_KeepsCurrentWhenStillOurs(t *testing.T) {
	l := &fakeLister{used: map[int]bool{19: true}}
	idx, err := AllocateIndex(context.Background(), l, 19)
	if err != nil {
		t.Fatalf("AllocateIndex: %v", err)
	}
	if idx != 19 {
		t.Errorf("reallocated an already-owned index: got %d, want 19", idx)
	}
}

func TestAllocateIndex_ExhaustedRangeErrors(t *testing.T) {
	used := map[int]bool{}
	for i := opkgTunMinIndex; i <= opkgTunMaxIndex; i++ {
		used[i] = true
	}
	if _, err := AllocateIndex(context.Background(), &fakeLister{used: used}, 0); err == nil {
		t.Fatal("expected exhaustion error")
	}
}

func TestPrepareInterface_NeverSetsAddressBeforeDeviceExists(t *testing.T) {
	f := &fakeNDMS{}
	if err := PrepareInterface(context.Background(), f, "OpkgTun18"); err != nil {
		t.Fatalf("PrepareInterface: %v", err)
	}
	want := []string{"create:OpkgTun18:private", "mtu:OpkgTun18"}
	if len(f.calls) != len(want) {
		t.Fatalf("calls = %v, want %v", f.calls, want)
	}
	for i := range want {
		if f.calls[i] != want[i] {
			t.Errorf("calls[%d] = %q, want %q", i, f.calls[i], want[i])
		}
	}
}

func TestActivateInterface_ACLThenUp(t *testing.T) {
	f := &fakeNDMS{}
	if err := ActivateInterface(context.Background(), f, "OpkgTun18"); err != nil {
		t.Fatalf("ActivateInterface: %v", err)
	}
	if len(f.calls) != 2 || f.calls[0] != "acl:OpkgTun18" || f.calls[1] != "up:OpkgTun18" {
		t.Errorf("calls = %v", f.calls)
	}
}

func TestInterfaceName_OS4FallsBackToTailscale0(t *testing.T) {
	cfg := DefaultConfig()
	cfg.OpkgTunIndex = 18
	if got := InterfaceName(cfg, true); got != "opkgtun18" {
		t.Errorf("OS5 name = %q", got)
	}
	if got := InterfaceName(cfg, false); got != "tailscale0" {
		t.Errorf("OS4 name = %q, want tailscale0", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tailscale/... -run "TestNames|TestAllocate|TestPrepare|TestActivate|TestInterfaceName" -v`
Expected: FAIL — undefined `opkgTunNDMSName`, `AllocateIndex`.

- [ ] **Step 3: Write minimal implementation**

```go
package tailscale

import (
	"context"
	"fmt"
)

// OpkgTun index range reserved for tailscale. Kept clear of the tunnel and
// wdtt ranges; AllocateIndex still consults the live index list, so an
// overlap with a hand-created interface cannot cause a silent collision.
const (
	opkgTunMinIndex = 20
	opkgTunMaxIndex = 29
	// tailscaleMTU leaves room for the WireGuard header tailscale adds.
	tailscaleMTU = 1280
	ifaceDescription = "awg-manager tailscale"
)

// NDMSInterfaces is the narrow slice of ndmscommand.InterfaceCommands this
// package needs. An interface, not the concrete type, so the lifecycle can be
// tested without an RCI endpoint.
type NDMSInterfaces interface {
	CreateOpkgTunWithSecurityLevel(ctx context.Context, name, description, securityLevel string) error
	SetMTU(ctx context.Context, name string, mtu int) error
	SetPermitAllACL(ctx context.Context, name string) error
	InterfaceUp(ctx context.Context, name string) error
	DeleteOpkgTun(ctx context.Context, name string) error
}

// IndexLister reports occupied OpkgTun indices (kernel ∪ NDMS).
type IndexLister interface {
	LiveOpkgTunIndices(ctx context.Context) (map[int]bool, error)
}

func opkgTunNDMSName(index int) string   { return fmt.Sprintf("OpkgTun%d", index) }
func opkgTunKernelName(index int) string { return fmt.Sprintf("opkgtun%d", index) }

// InterfaceName resolves the kernel device tailscaled binds to. OS 4.x has no
// NDMS, so there is no slot to allocate and a plain tailscale0 is used.
func InterfaceName(cfg Config, isOS5 bool) string {
	if !isOS5 {
		return "tailscale0"
	}
	return opkgTunKernelName(cfg.OpkgTunIndex)
}

// AllocateIndex picks a free OpkgTun index, keeping `current` when it is
// already allocated to us (a live index we own must not be re-picked, or a
// restart would strand the previous interface).
func AllocateIndex(ctx context.Context, l IndexLister, current int) (int, error) {
	used, err := l.LiveOpkgTunIndices(ctx)
	if err != nil {
		return 0, fmt.Errorf("список занятых OpkgTun: %w", err)
	}
	if current >= opkgTunMinIndex && current <= opkgTunMaxIndex {
		return current, nil
	}
	for i := opkgTunMinIndex; i <= opkgTunMaxIndex; i++ {
		if !used[i] {
			return i, nil
		}
	}
	return 0, fmt.Errorf("нет свободного слота OpkgTun в диапазоне %d-%d", opkgTunMinIndex, opkgTunMaxIndex)
}

// PrepareInterface registers the slot in NDMS BEFORE tailscaled creates the
// kernel device. The address is deliberately NOT set here: an interface with
// a configured `ip address` but no kernel address drives ndm into an endless
// nginx-reload loop that hangs RCI (stand-verified 2026-07-15, PR #544 —
// see internal/wdtt/ndms_iface.go:177).
func PrepareInterface(ctx context.Context, n NDMSInterfaces, ndmsName string) error {
	if err := n.CreateOpkgTunWithSecurityLevel(ctx, ndmsName, ifaceDescription, "private"); err != nil {
		return fmt.Errorf("создание %s: %w", ndmsName, err)
	}
	if err := n.SetMTU(ctx, ndmsName, tailscaleMTU); err != nil {
		return fmt.Errorf("mtu %s: %w", ndmsName, err)
	}
	return nil
}

// ActivateInterface applies ACL and brings the interface up once the kernel
// device exists. The address is left to tailscaled.
func ActivateInterface(ctx context.Context, n NDMSInterfaces, ndmsName string) error {
	if err := n.SetPermitAllACL(ctx, ndmsName); err != nil {
		return fmt.Errorf("firewall permit %s: %w", ndmsName, err)
	}
	if err := n.InterfaceUp(ctx, ndmsName); err != nil {
		return fmt.Errorf("iface up %s: %w", ndmsName, err)
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tailscale/... -v`
Expected: PASS.

- [ ] **Step 5: Reconcile with the Task 0 spike answer**

If the spike found that tailscaled will NOT attach to an NDMS-pre-created device, `PrepareInterface` must not run before the daemon starts. In that case change the service ordering in Task 10 to: start daemon → wait for `/sys/class/net/<iface>` → `CreateOpkgTunWithSecurityLevel` (NDMS adopts the existing device) → `ActivateInterface`. The functions above stay unchanged; only the call order in `service.go` moves. Record which ordering was chosen in a comment on `PrepareInterface`.

- [ ] **Step 6: Commit**

```bash
git add internal/tailscale/ndms_iface.go internal/tailscale/ndms_iface_test.go
git commit -m "feat(tailscale): NDMS OpkgTun slot lifecycle"
```

---

### Task 8: WAN egress — forwarding and NAT rules

**Files:**
- Create: `internal/tailscale/egress.go` (pure rule rendering, no build tag)
- Create: `internal/tailscale/egress_linux.go` (`//go:build linux`)
- Create: `internal/tailscale/egress_other.go` (`//go:build !linux`)
- Test: `internal/tailscale/egress_test.go`

**Interfaces:**
- Consumes: `Config`, `TailnetCGNAT` (Task 1); `iptables.Run` / `iptables.RunOutput` (`internal/sys/iptables/iptables.go:27,117`).
- Produces: `const natComment = "AWGM_TAILSCALE"`, `func wanRules(iface, wanDev string) [][]string`, `func removalRules(iface, wanDev string) [][]string`, `func masqueradeOutDev(natOut string) string`, `func natPresent(natOut, fwdOut, iface, wanDev string) bool`, and the linux-only `applyWANEgress(ctx, iface, wanDev string) error` / `removeEgress(ctx, iface, wanDev string) error`.

- [ ] **Step 1: Write the failing test**

```go
package tailscale

import (
	"strings"
	"testing"
)

func flatten(rules [][]string) []string {
	out := make([]string, len(rules))
	for i, r := range rules {
		out[i] = strings.Join(r, " ")
	}
	return out
}

// hasRule matches on a prefix and a suffix rather than one contiguous string,
// because the comment tokens sit BETWEEN the match and the -j target.
func hasRule(rules []string, prefix, suffix string) bool {
	for _, r := range rules {
		if strings.HasPrefix(r, prefix) && strings.HasSuffix(r, suffix) {
			return true
		}
	}
	return false
}

func TestWANRules_ForwardBothDirectionsAndMasquerade(t *testing.T) {
	got := flatten(wanRules("opkgtun20", "ppp0"))
	cases := []struct{ prefix, suffix string }{
		{"-A FORWARD -i opkgtun20", "-j ACCEPT"},
		{"-A FORWARD -o opkgtun20 -m state --state RELATED,ESTABLISHED", "-j ACCEPT"},
		{"-t nat -A POSTROUTING -s 100.64.0.0/10 -o ppp0", "-j MASQUERADE"},
	}
	for _, c := range cases {
		if !hasRule(got, c.prefix, c.suffix) {
			t.Errorf("missing rule %q…%q in:\n%s", c.prefix, c.suffix, strings.Join(got, "\n"))
		}
	}
}

func TestWANRules_EveryRuleCarriesTheComment(t *testing.T) {
	for _, r := range flatten(wanRules("opkgtun20", "ppp0")) {
		if !strings.Contains(r, natComment) {
			t.Errorf("rule without %s comment: %s", natComment, r)
		}
	}
}

func TestRemovalRules_MirrorApplyRulesWithDelete(t *testing.T) {
	apply := wanRules("opkgtun20", "ppp0")
	remove := removalRules("opkgtun20", "ppp0")
	if len(apply) != len(remove) {
		t.Fatalf("apply has %d rules, removal has %d", len(apply), len(remove))
	}
	for i, r := range flatten(remove) {
		if !strings.Contains(r, "-D ") {
			t.Errorf("removal rule %d is not a delete: %s", i, r)
		}
	}
}

func TestMasqueradeOutDev(t *testing.T) {
	natOut := "-P POSTROUTING ACCEPT\n" +
		"-A POSTROUTING -s 100.64.0.0/10 -o ppp0 -m comment --comment AWGM_TAILSCALE -j MASQUERADE\n"
	if got := masqueradeOutDev(natOut); got != "ppp0" {
		t.Errorf("masqueradeOutDev = %q, want ppp0", got)
	}
	if got := masqueradeOutDev("-P POSTROUTING ACCEPT\n"); got != "" {
		t.Errorf("want empty for absent rule, got %q", got)
	}
}

func TestNATPresent_DetectsStaleDeviceAfterWANFailover(t *testing.T) {
	fwd := "-A FORWARD -i opkgtun20 -m comment --comment AWGM_TAILSCALE -j ACCEPT\n"
	// Rule survives the failover but points at the OLD device.
	natOld := "-A POSTROUTING -s 100.64.0.0/10 -o ppp0 -m comment --comment AWGM_TAILSCALE -j MASQUERADE\n"
	if natPresent(natOld, fwd, "opkgtun20", "eth3") {
		t.Error("stale MASQUERADE device must count as absent so reconcile reinstalls")
	}
	natNew := "-A POSTROUTING -s 100.64.0.0/10 -o eth3 -m comment --comment AWGM_TAILSCALE -j MASQUERADE\n"
	if !natPresent(natNew, fwd, "opkgtun20", "eth3") {
		t.Error("matching rules must count as present")
	}
}

func TestNATPresent_MissingForwardCountsAsAbsent(t *testing.T) {
	nat := "-A POSTROUTING -s 100.64.0.0/10 -o eth3 -m comment --comment AWGM_TAILSCALE -j MASQUERADE\n"
	if natPresent(nat, "-P FORWARD ACCEPT\n", "opkgtun20", "eth3") {
		t.Error("absent FORWARD rules must count as absent")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tailscale/... -run "TestWANRules|TestRemoval|TestMasq|TestNATPresent" -v`
Expected: FAIL — undefined `wanRules`.

- [ ] **Step 3: Write minimal implementation**

`internal/tailscale/egress.go` — pure rendering, so the whole rule surface is testable on darwin:

```go
package tailscale

import "strings"

// natComment tags every rule this package installs so the reconciler can
// find and verify them, mirroring entwareNATComment in internal/wdtt.
const natComment = "AWGM_TAILSCALE"

// wanRules renders the forwarding + NAT rules for WAN egress. Rendering is
// separated from execution so the rule surface is unit-testable without root
// or a live iptables.
func wanRules(iface, wanDev string) [][]string {
	comment := []string{"-m", "comment", "--comment", natComment}
	return [][]string{
		append(append([]string{"-A", "FORWARD", "-i", iface}, comment...), "-j", "ACCEPT"),
		append(append([]string{"-A", "FORWARD", "-o", iface, "-m", "state", "--state", "RELATED,ESTABLISHED"}, comment...), "-j", "ACCEPT"),
		append(append([]string{"-t", "nat", "-A", "POSTROUTING", "-s", TailnetCGNAT, "-o", wanDev}, comment...), "-j", "MASQUERADE"),
	}
}

// removalRules is wanRules with every -A turned into -D, so apply and remove
// can never drift apart.
func removalRules(iface, wanDev string) [][]string {
	rules := wanRules(iface, wanDev)
	out := make([][]string, len(rules))
	for i, r := range rules {
		cp := append([]string(nil), r...)
		for j, tok := range cp {
			if tok == "-A" {
				cp[j] = "-D"
			}
		}
		out[i] = cp
	}
	return out
}

// masqueradeOutDev returns the `-o <dev>` of our MASQUERADE rule, or "".
func masqueradeOutDev(natOut string) string {
	for _, line := range strings.Split(natOut, "\n") {
		if !strings.Contains(line, natComment) {
			continue
		}
		fields := strings.Fields(line)
		for i, f := range fields {
			if f == "-o" && i+1 < len(fields) {
				return fields[i+1]
			}
		}
	}
	return ""
}

// natPresent reports whether our rules are installed AND the MASQUERADE still
// exits through wanDev. After a WAN failover the rule keeps its comment but
// points at a dead device — treating that as absent is what makes the
// reconciler reinstall it.
func natPresent(natOut, fwdOut, iface, wanDev string) bool {
	if !strings.Contains(fwdOut, natComment) || !strings.Contains(fwdOut, iface) {
		return false
	}
	return masqueradeOutDev(natOut) == wanDev
}
```

`internal/tailscale/egress_linux.go`:

```go
//go:build linux

package tailscale

import (
	"context"
	"fmt"
	"strings"

	"github.com/hoaxisr/awg-manager/internal/sys/exec"
	"github.com/hoaxisr/awg-manager/internal/sys/iptables"
)

// applyWANEgress installs forwarding + MASQUERADE for exit-node traffic.
// Idempotent: removal runs first so a re-apply after a device change cannot
// stack duplicate rules.
func applyWANEgress(ctx context.Context, iface, wanDev string) error {
	if wanDev == "" {
		dev, err := defaultWANDev(ctx)
		if err != nil {
			return err
		}
		wanDev = dev
	}
	removeEgress(ctx, iface, wanDev)
	if err := enableIPForward(ctx); err != nil {
		return err
	}
	for _, rule := range wanRules(iface, wanDev) {
		if err := iptables.Run(ctx, rule...); err != nil {
			return fmt.Errorf("iptables %s: %w", strings.Join(rule, " "), err)
		}
	}
	return nil
}

// removeEgress is best-effort: a rule that is already gone is not an error.
func removeEgress(ctx context.Context, iface, wanDev string) {
	for _, rule := range removalRules(iface, wanDev) {
		_ = iptables.Run(ctx, rule...)
	}
}

// egressPresent re-reads the live tables for the reconciler.
func egressPresent(ctx context.Context, iface, wanDev string) bool {
	natOut, err1 := iptables.RunOutput(ctx, "-t", "nat", "-S", "POSTROUTING")
	fwdOut, err2 := iptables.RunOutput(ctx, "-S", "FORWARD")
	if err1 != nil || err2 != nil {
		return false
	}
	return natPresent(natOut, fwdOut, iface, wanDev)
}

func enableIPForward(ctx context.Context) error {
	res, err := exec.Run(ctx, "/opt/bin/sysctl", "-w", "net.ipv4.ip_forward=1")
	if err != nil {
		return fmt.Errorf("ip_forward: %w", exec.FormatError(res, err))
	}
	return nil
}

// defaultWANDev resolves the current default-route device.
func defaultWANDev(ctx context.Context) (string, error) {
	res, err := exec.Run(ctx, "/opt/sbin/ip", "route", "show", "default")
	if err != nil {
		return "", fmt.Errorf("default route: %w", exec.FormatError(res, err))
	}
	fields := strings.Fields(res.Stdout)
	for i, f := range fields {
		if f == "dev" && i+1 < len(fields) {
			return fields[i+1], nil
		}
	}
	return "", fmt.Errorf("не найден маршрут по умолчанию")
}
```

`internal/tailscale/egress_other.go`:

```go
//go:build !linux

package tailscale

import (
	"context"
	"errors"
)

var errNotLinux = errors.New("egress-правила поддерживаются только на Linux")

func applyWANEgress(context.Context, string, string) error { return errNotLinux }
func removeEgress(context.Context, string, string)         {}
func egressPresent(context.Context, string, string) bool   { return false }
func defaultWANDev(context.Context) (string, error)        { return "", errNotLinux }
func enableIPForward(context.Context) error                { return errNotLinux }
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tailscale/... -v && GOOS=linux go build ./internal/tailscale/`
Expected: PASS, and the linux build succeeds.

- [ ] **Step 5: Commit**

```bash
git add internal/tailscale/egress.go internal/tailscale/egress_linux.go internal/tailscale/egress_other.go internal/tailscale/egress_test.go
git commit -m "feat(tailscale): WAN egress forwarding and NAT"
```

---

### Task 9: Tunnel egress and drift reconciler

**Files:**
- Create: `internal/tailscale/egress_tunnel.go`
- Create: `internal/tailscale/egress_reconcile.go`
- Test: `internal/tailscale/egress_tunnel_test.go`
- Test: `internal/tailscale/egress_reconcile_test.go`

**Interfaces:**
- Consumes: `Config.EgressTunnelID`, `Config.TunnelFallback`, `Config.RouteTable` (Task 1); `egressPresent`, `applyWANEgress`, `removeEgress` (Task 8).
- Produces: `type RouteOps interface` (satisfied by the existing `internal/tunnel/ops` client-route operator), `func ApplyTunnelEgress(ctx, RouteOps, iface, tunnelIface string, table int) error`, `func OnEgressTunnelDown(ctx, RouteOps, fallback string, table int) error`, `func (s *Service) StartEgressReconciler(ctx)`.

- [ ] **Step 1: Write the failing test**

```go
package tailscale

import (
	"context"
	"fmt"
	"testing"
)

type fakeRouteOps struct {
	calls []string
	used  map[int]bool
}

func (f *fakeRouteOps) ListUsedRoutingTables(context.Context) (map[int]bool, error) {
	return f.used, nil
}
func (f *fakeRouteOps) SetupClientRouteTable(_ context.Context, iface string, table int) error {
	f.calls = append(f.calls, fmt.Sprintf("setup:%s:%d", iface, table))
	return nil
}
func (f *fakeRouteOps) AddClientRule(_ context.Context, src string, table int) error {
	f.calls = append(f.calls, fmt.Sprintf("add:%s:%d", src, table))
	return nil
}
func (f *fakeRouteOps) RemoveClientRule(_ context.Context, src string, table int) error {
	f.calls = append(f.calls, fmt.Sprintf("remove:%s:%d", src, table))
	return nil
}
func (f *fakeRouteOps) CleanupClientRouteTable(_ context.Context, table int) error {
	f.calls = append(f.calls, fmt.Sprintf("cleanup:%d", table))
	return nil
}

func TestApplyTunnelEgress_SetupThenRuleOnCGNATRange(t *testing.T) {
	f := &fakeRouteOps{}
	if err := ApplyTunnelEgress(context.Background(), f, "opkgtun20", "awgm0", 220); err != nil {
		t.Fatalf("ApplyTunnelEgress: %v", err)
	}
	want := []string{"setup:awgm0:220", "add:100.64.0.0/10:220"}
	if len(f.calls) != len(want) {
		t.Fatalf("calls = %v, want %v", f.calls, want)
	}
	for i := range want {
		if f.calls[i] != want[i] {
			t.Errorf("calls[%d] = %q, want %q", i, f.calls[i], want[i])
		}
	}
}

func TestOnEgressTunnelDown_DropKeepsTheRuleAsKillSwitch(t *testing.T) {
	f := &fakeRouteOps{}
	if err := OnEgressTunnelDown(context.Background(), f, "drop", 220); err != nil {
		t.Fatalf("OnEgressTunnelDown: %v", err)
	}
	if len(f.calls) != 0 {
		t.Errorf("drop must leave rules untouched, got %v", f.calls)
	}
}

func TestOnEgressTunnelDown_BypassRemovesRuleAndTable(t *testing.T) {
	f := &fakeRouteOps{}
	if err := OnEgressTunnelDown(context.Background(), f, "bypass", 220); err != nil {
		t.Fatalf("OnEgressTunnelDown: %v", err)
	}
	want := []string{"remove:100.64.0.0/10:220", "cleanup:220"}
	if len(f.calls) != len(want) {
		t.Fatalf("calls = %v, want %v", f.calls, want)
	}
	for i := range want {
		if f.calls[i] != want[i] {
			t.Errorf("calls[%d] = %q, want %q", i, f.calls[i], want[i])
		}
	}
}

func TestAllocateRouteTable_SkipsUsed(t *testing.T) {
	f := &fakeRouteOps{used: map[int]bool{routeTableMin: true, routeTableMin + 1: true}}
	got, err := AllocateRouteTable(context.Background(), f, 0)
	if err != nil {
		t.Fatalf("AllocateRouteTable: %v", err)
	}
	if got == routeTableMin || got == routeTableMin+1 {
		t.Errorf("allocated an in-use table %d", got)
	}
}

func TestAllocateRouteTable_KeepsCurrent(t *testing.T) {
	f := &fakeRouteOps{used: map[int]bool{routeTableMin: true}}
	got, err := AllocateRouteTable(context.Background(), f, routeTableMin)
	if err != nil {
		t.Fatalf("AllocateRouteTable: %v", err)
	}
	if got != routeTableMin {
		t.Errorf("got %d, want the already-owned table %d", got, routeTableMin)
	}
}
```

`egress_reconcile_test.go`:

```go
package tailscale

import (
	"context"
	"testing"
)

func TestReconcileOnce_ReinstallsWhenRulesDrifted(t *testing.T) {
	applied := 0
	r := &egressReconciler{
		present: func(context.Context) bool { return false },
		apply:   func(context.Context) error { applied++; return nil },
		enabled: func() bool { return true },
	}
	r.reconcileOnce(context.Background())
	if applied != 1 {
		t.Errorf("applied %d times, want 1", applied)
	}
}

func TestReconcileOnce_NoOpWhenRulesIntact(t *testing.T) {
	applied := 0
	r := &egressReconciler{
		present: func(context.Context) bool { return true },
		apply:   func(context.Context) error { applied++; return nil },
		enabled: func() bool { return true },
	}
	r.reconcileOnce(context.Background())
	if applied != 0 {
		t.Errorf("applied %d times, want 0", applied)
	}
}

func TestReconcileOnce_SkipsWhenFeatureDisabled(t *testing.T) {
	applied := 0
	r := &egressReconciler{
		present: func(context.Context) bool { return false },
		apply:   func(context.Context) error { applied++; return nil },
		enabled: func() bool { return false },
	}
	r.reconcileOnce(context.Background())
	if applied != 0 {
		t.Errorf("disabled feature must not install rules, applied %d", applied)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tailscale/... -run "TestApplyTunnel|TestOnEgress|TestAllocateRoute|TestReconcile" -v`
Expected: FAIL — undefined `ApplyTunnelEgress`, `egressReconciler`.

- [ ] **Step 3: Write minimal implementation**

`internal/tailscale/egress_tunnel.go`:

```go
package tailscale

import (
	"context"
	"fmt"
)

// Routing-table range for tailscale exit-node egress. Disjoint from the
// clientroute allocations, and AllocateRouteTable still consults the live
// list before picking.
const (
	routeTableMin = 220
	routeTableMax = 229
)

// RouteOps is the policy-routing surface, satisfied by the client-route
// operator in internal/tunnel/ops. Reused rather than reimplemented: these
// are the same `ip rule` / `ip route` primitives, and one implementation
// means one place where the LAN-bypass route can be wrong.
type RouteOps interface {
	ListUsedRoutingTables(ctx context.Context) (map[int]bool, error)
	SetupClientRouteTable(ctx context.Context, kernelIface string, tableNum int) error
	AddClientRule(ctx context.Context, clientIP string, tableNum int) error
	RemoveClientRule(ctx context.Context, clientIP string, tableNum int) error
	CleanupClientRouteTable(ctx context.Context, tableNum int) error
}

// AllocateRouteTable picks a free table, keeping `current` when already ours.
func AllocateRouteTable(ctx context.Context, ops RouteOps, current int) (int, error) {
	if current >= routeTableMin && current <= routeTableMax {
		return current, nil
	}
	used, err := ops.ListUsedRoutingTables(ctx)
	if err != nil {
		return 0, fmt.Errorf("список таблиц маршрутизации: %w", err)
	}
	for i := routeTableMin; i <= routeTableMax; i++ {
		if !used[i] {
			return i, nil
		}
	}
	return 0, fmt.Errorf("нет свободной таблицы маршрутизации в диапазоне %d-%d", routeTableMin, routeTableMax)
}

// ApplyTunnelEgress points a dedicated table at the chosen tunnel and steers
// the whole tailnet CGNAT range into it.
func ApplyTunnelEgress(ctx context.Context, ops RouteOps, iface, tunnelIface string, table int) error {
	if err := ops.SetupClientRouteTable(ctx, tunnelIface, table); err != nil {
		return fmt.Errorf("таблица %d: %w", table, err)
	}
	if err := ops.AddClientRule(ctx, TailnetCGNAT, table); err != nil {
		return fmt.Errorf("ip rule %s: %w", TailnetCGNAT, err)
	}
	return nil
}

// OnEgressTunnelDown applies the configured fallback. "drop" deliberately
// leaves the ip rule in place: the table's default route is gone, so packets
// are blackholed instead of silently leaking out the bare WAN — an exit node
// quietly falling back to the home IP is exactly the failure a user would
// never notice.
func OnEgressTunnelDown(ctx context.Context, ops RouteOps, fallback string, table int) error {
	if fallback != "bypass" {
		return nil
	}
	if err := ops.RemoveClientRule(ctx, TailnetCGNAT, table); err != nil {
		return err
	}
	return ops.CleanupClientRouteTable(ctx, table)
}
```

`internal/tailscale/egress_reconcile.go`:

```go
package tailscale

import (
	"context"
	"time"
)

// egressReconcileInterval matches internal/wdtt: NDMS and the sing-box router
// both flush POSTROUTING/FORWARD, so our rules need periodic reinstatement.
const egressReconcileInterval = 15 * time.Second

// egressReconciler re-applies egress rules when they drift. The three
// function fields keep the loop testable without iptables.
type egressReconciler struct {
	present func(context.Context) bool
	apply   func(context.Context) error
	enabled func() bool
	onError func(error)
}

func (r *egressReconciler) reconcileOnce(ctx context.Context) {
	if r.enabled == nil || !r.enabled() {
		return
	}
	if r.present != nil && r.present(ctx) {
		return
	}
	if err := r.apply(ctx); err != nil && r.onError != nil {
		r.onError(err)
	}
}

func (r *egressReconciler) run(ctx context.Context) {
	ticker := time.NewTicker(egressReconcileInterval)
	defer ticker.Stop()
	r.reconcileOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.reconcileOnce(ctx)
		}
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tailscale/... -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tailscale/egress_tunnel.go internal/tailscale/egress_reconcile.go internal/tailscale/egress_tunnel_test.go internal/tailscale/egress_reconcile_test.go
git commit -m "feat(tailscale): tunnel egress policy routing and drift reconciler"
```

---

### Task 10: Service orchestration and watchdog

**Files:**
- Create: `internal/tailscale/service.go`
- Create: `internal/tailscale/watchdog.go`
- Test: `internal/tailscale/service_test.go`
- Test: `internal/tailscale/watchdog_test.go`

**Interfaces:**
- Consumes: everything from Tasks 1-9.
- Produces: `type Service`, `type Deps`, `New(Deps) *Service`, and the methods the API layer calls: `(*Service).Status(ctx) FullStatus`, `(*Service).GetConfig() (PublicConfig, error)`, `(*Service).UpdateConfig(ctx, PublicConfig, authKey string) error`, `(*Service).Up(ctx) (loginURL string, err error)`, `(*Service).Down(ctx) error`, `(*Service).Logout(ctx) error`, `(*Service).Install(ctx) error`, `(*Service).Logs(n int) string`, `(*Service).StartWorkers(ctx)`.

- [ ] **Step 1: Write the failing test**

```go
package tailscale

import (
	"context"
	"strings"
	"testing"
)

func newTestService(t *testing.T) (*Service, *fakeRunner) {
	t.Helper()
	r := &fakeRunner{}
	svc := New(Deps{
		Store:     NewStore(t.TempDir()),
		Installer: NewInstaller("aarch64-3.10", t.TempDir(), &fakeDownloader{payload: []byte("x")}),
		RunCLI:    r.Run,
		IsOS5:     func() bool { return true },
		Preflight: func(context.Context) PreflightResult { return PreflightResult{OK: true} },
	})
	return svc, r
}

type fakeRunner struct {
	lastArgs []string
	stdout   string
	err      error
}

func (f *fakeRunner) Run(_ context.Context, args ...string) (string, error) {
	f.lastArgs = args
	return f.stdout, f.err
}

func TestService_UpReturnsLoginURL(t *testing.T) {
	svc, r := newTestService(t)
	r.stdout = "To authenticate, visit:\n\thttps://login.tailscale.com/a/deadbeef\n"
	url, err := svc.Up(context.Background())
	if err != nil {
		t.Fatalf("Up: %v", err)
	}
	if url != "https://login.tailscale.com/a/deadbeef" {
		t.Errorf("login URL = %q", url)
	}
}

func TestService_UpIsBlockedByPreflight(t *testing.T) {
	svc, _ := newTestService(t)
	svc.preflight = func(context.Context) PreflightResult {
		return PreflightResult{OK: false, Blockers: []Blocker{{Code: BlockerNoTun, Message: "нет tun"}}}
	}
	if _, err := svc.Up(context.Background()); err == nil {
		t.Fatal("expected preflight to block Up")
	} else if !strings.Contains(err.Error(), "нет tun") {
		t.Errorf("error must carry the blocker message, got %v", err)
	}
}

func TestService_DownSetsManuallyStopped(t *testing.T) {
	svc, _ := newTestService(t)
	if err := svc.Down(context.Background()); err != nil {
		t.Fatalf("Down: %v", err)
	}
	cfg, _ := svc.store.Load()
	if !cfg.ManuallyStopped {
		t.Error("Down must persist ManuallyStopped so the watchdog respects it across restarts")
	}
}

func TestService_UpClearsManuallyStopped(t *testing.T) {
	svc, _ := newTestService(t)
	_ = svc.Down(context.Background())
	_, _ = svc.Up(context.Background())
	cfg, _ := svc.store.Load()
	if cfg.ManuallyStopped {
		t.Error("Up must clear ManuallyStopped")
	}
}

func TestService_UpdateConfigKeepsExistingAuthKeyWhenBlank(t *testing.T) {
	svc, _ := newTestService(t)
	if err := svc.UpdateConfig(context.Background(), PublicConfig{EgressMode: EgressWAN, TunnelFallback: "drop"}, "tskey-auth-first"); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
	// A blank key from the UI means "unchanged", not "erase".
	if err := svc.UpdateConfig(context.Background(), PublicConfig{EgressMode: EgressWAN, TunnelFallback: "drop"}, ""); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
	cfg, _ := svc.store.Load()
	if cfg.AuthKey != "tskey-auth-first" {
		t.Errorf("auth key = %q, want it preserved", cfg.AuthKey)
	}
}

func TestService_UpdateConfigRejectsTunnelEgressWithoutTunnel(t *testing.T) {
	svc, _ := newTestService(t)
	err := svc.UpdateConfig(context.Background(), PublicConfig{EgressMode: EgressTunnel, TunnelFallback: "drop"}, "")
	if err == nil {
		t.Fatal("tunnel egress without a tunnel id must be rejected")
	}
}

func TestService_UpdateConfigRejectsInvalidRoute(t *testing.T) {
	svc, _ := newTestService(t)
	err := svc.UpdateConfig(context.Background(), PublicConfig{
		EgressMode:      EgressWAN,
		TunnelFallback:  "drop",
		AdvertiseRoutes: []string{"not-a-cidr"},
	}, "")
	if err == nil {
		t.Fatal("invalid CIDR must be rejected")
	}
}

func TestService_StatusCarriesInstallAndPreflight(t *testing.T) {
	svc, _ := newTestService(t)
	st := svc.Status(context.Background())
	if st.Install.RequiredVersion != PinnedVersion {
		t.Errorf("Install.RequiredVersion = %q", st.Install.RequiredVersion)
	}
	if !st.Preflight.OK {
		t.Errorf("Preflight = %+v", st.Preflight)
	}
}
```

`watchdog_test.go`:

```go
package tailscale

import (
	"context"
	"testing"
	"time"
)

func TestWatchdog_RestartsDeadDaemon(t *testing.T) {
	restarts := 0
	w := &watchdog{
		shouldRun: func() bool { return true },
		isRunning: func() bool { return false },
		restart:   func(context.Context) error { restarts++; return nil },
		backoff:   newRestartBackoff(),
	}
	w.tick(context.Background())
	if restarts != 1 {
		t.Errorf("restarts = %d, want 1", restarts)
	}
}

func TestWatchdog_RespectsManualStop(t *testing.T) {
	restarts := 0
	w := &watchdog{
		shouldRun: func() bool { return false },
		isRunning: func() bool { return false },
		restart:   func(context.Context) error { restarts++; return nil },
		backoff:   newRestartBackoff(),
	}
	w.tick(context.Background())
	if restarts != 0 {
		t.Errorf("a manually stopped daemon must not be resurrected, restarts = %d", restarts)
	}
}

func TestRestartBackoff_GrowsThenResets(t *testing.T) {
	b := newRestartBackoff()
	first := b.next()
	second := b.next()
	if second <= first {
		t.Errorf("backoff must grow: %v then %v", first, second)
	}
	if max := b.next(); max > maxRestartBackoff {
		t.Errorf("backoff %v exceeds cap %v", max, maxRestartBackoff)
	}
	b.reset()
	if b.next() != first {
		t.Errorf("reset must return to the initial delay")
	}
}

func TestWatchdog_BackoffSuppressesRapidRestarts(t *testing.T) {
	restarts := 0
	w := &watchdog{
		shouldRun: func() bool { return true },
		isRunning: func() bool { return false },
		restart:   func(context.Context) error { restarts++; return nil },
		backoff:   newRestartBackoff(),
		now:       func() time.Time { return time.Unix(1000, 0) },
	}
	w.tick(context.Background())
	w.tick(context.Background()) // same instant — still inside the backoff window
	if restarts != 1 {
		t.Errorf("restarts = %d, want 1 (second tick suppressed by backoff)", restarts)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tailscale/... -run "TestService|TestWatchdog|TestRestartBackoff" -v`
Expected: FAIL — undefined `New`, `Deps`, `watchdog`.

- [ ] **Step 3: Write the watchdog**

```go
package tailscale

import (
	"context"
	"time"
)

const (
	watchdogInterval    = 20 * time.Second
	initialRestartDelay = 5 * time.Second
	maxRestartBackoff   = 5 * time.Minute
)

// restartBackoff grows the delay between restart attempts so a daemon that
// crashes on startup does not spin the router's CPU.
type restartBackoff struct {
	current time.Duration
}

func newRestartBackoff() *restartBackoff {
	return &restartBackoff{current: initialRestartDelay}
}

func (b *restartBackoff) next() time.Duration {
	d := b.current
	if b.current < maxRestartBackoff {
		b.current *= 2
		if b.current > maxRestartBackoff {
			b.current = maxRestartBackoff
		}
	}
	return d
}

func (b *restartBackoff) reset() { b.current = initialRestartDelay }

type watchdog struct {
	shouldRun func() bool
	isRunning func() bool
	restart   func(context.Context) error
	backoff   *restartBackoff
	now       func() time.Time
	onError   func(error)

	nextAttempt time.Time
}

func (w *watchdog) tick(ctx context.Context) {
	if !w.shouldRun() {
		return
	}
	if w.isRunning() {
		w.backoff.reset()
		w.nextAttempt = time.Time{}
		return
	}
	now := time.Now
	if w.now != nil {
		now = w.now
	}
	if !w.nextAttempt.IsZero() && now().Before(w.nextAttempt) {
		return
	}
	delay := w.backoff.next()
	w.nextAttempt = now().Add(delay)
	if err := w.restart(ctx); err != nil && w.onError != nil {
		w.onError(err)
	}
}

func (w *watchdog) run(ctx context.Context) {
	ticker := time.NewTicker(watchdogInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.tick(ctx)
		}
	}
}
```

- [ ] **Step 4: Write the service**

`internal/tailscale/service.go`. Key rules encoded here: preflight blocks `Up`; `UpdateConfig` validates before persisting; a blank auth key means "unchanged"; `Up`/`Down` own the `ManuallyStopped` flag.

```go
package tailscale

import (
	"context"
	"fmt"
	"net/netip"
	"strings"

	"github.com/hoaxisr/awg-manager/internal/logging"
)

// CLIRunner executes the tailscale CLI and returns combined output.
type CLIRunner func(ctx context.Context, args ...string) (string, error)

// FullStatus is the single payload GET /api/tailscale/status returns.
type FullStatus struct {
	Config    PublicConfig    `json:"config"`
	Install   InstallStatus   `json:"install"`
	Preflight PreflightResult `json:"preflight"`
	Node      Status          `json:"node"`
	Running   bool            `json:"running"`
	// PendingApproval surfaces "advertised but not approved in the admin
	// console" — without it the UI looks healthy while no client can route.
	PendingApproval bool   `json:"pendingApproval"`
	LastError       string `json:"lastError,omitempty"`
}

type Deps struct {
	Store     *Store
	Installer *Installer
	LocalAPI  *LocalAPI
	Process   *process
	RunCLI    CLIRunner
	IsOS5     func() bool
	Preflight func(context.Context) PreflightResult
	AppLogger logging.AppLogger
}

type Service struct {
	store     *Store
	installer *Installer
	local     *LocalAPI
	proc      *process
	runCLI    CLIRunner
	isOS5     func() bool
	preflight func(context.Context) PreflightResult
	appLog    *logging.ScopedLogger
	socket    string
}

func New(d Deps) *Service {
	return &Service{
		store:     d.Store,
		installer: d.Installer,
		local:     d.LocalAPI,
		proc:      d.Process,
		runCLI:    d.RunCLI,
		isOS5:     d.IsOS5,
		preflight: d.Preflight,
		appLog:    logging.NewScopedLogger(d.AppLogger, logging.GroupRouting, SubTailscale),
		socket:    DefaultSocketPath,
	}
}

// Up applies desired state through `tailscale up` and returns the interactive
// login URL when the node still needs authentication.
func (s *Service) Up(ctx context.Context) (string, error) {
	if res := s.preflight(ctx); !res.OK {
		msgs := make([]string, 0, len(res.Blockers))
		for _, b := range res.Blockers {
			msgs = append(msgs, b.Message)
		}
		return "", fmt.Errorf("проверки не пройдены: %s", strings.Join(msgs, "; "))
	}
	cfg, err := s.store.Load()
	if err != nil {
		return "", err
	}
	cfg.ManuallyStopped = false
	cfg.Enabled = true
	if err := s.store.Save(cfg); err != nil {
		return "", err
	}
	out, err := s.runCLI(ctx, UpArgs(cfg, s.socket)...)
	if err != nil {
		return "", fmt.Errorf("tailscale up: %s", Scrub(err.Error()))
	}
	return ExtractLoginURL(out), nil
}

// Down stops the node and records the intent so the watchdog does not
// resurrect it after an awg-manager restart.
func (s *Service) Down(ctx context.Context) error {
	cfg, err := s.store.Load()
	if err != nil {
		return err
	}
	cfg.ManuallyStopped = true
	cfg.Enabled = false
	if err := s.store.Save(cfg); err != nil {
		return err
	}
	if s.runCLI != nil {
		if _, err := s.runCLI(ctx, DownArgs(s.socket)...); err != nil {
			s.appLog.Warn("down", "tailscale", Scrub(err.Error()))
		}
	}
	return nil
}

// UpdateConfig validates then persists. Validation happens before any write
// so a rejected form leaves the previous state intact.
func (s *Service) UpdateConfig(ctx context.Context, in PublicConfig, authKey string) error {
	for _, r := range in.AdvertiseRoutes {
		if _, err := netip.ParsePrefix(strings.TrimSpace(r)); err != nil {
			return fmt.Errorf("некорректная подсеть %q", r)
		}
	}
	if in.EgressMode == EgressTunnel && strings.TrimSpace(in.EgressTunnelID) == "" {
		return fmt.Errorf("для egress через туннель нужно выбрать туннель")
	}
	cfg, err := s.store.Load()
	if err != nil {
		return err
	}
	cfg.Hostname = in.Hostname
	cfg.LoginServer = in.LoginServer
	cfg.AdvertiseRoutes = in.AdvertiseRoutes
	cfg.ExitNode = in.ExitNode
	cfg.EgressMode = in.EgressMode
	cfg.EgressTunnelID = in.EgressTunnelID
	cfg.TunnelFallback = in.TunnelFallback
	// A blank key from the UI means "unchanged" — the form never receives the
	// stored key back, so echoing an empty string must not erase it.
	if strings.TrimSpace(authKey) != "" {
		cfg.AuthKey = strings.TrimSpace(authKey)
	}
	return s.store.Save(cfg)
}

func (s *Service) GetConfig() (PublicConfig, error) {
	cfg, err := s.store.Load()
	if err != nil {
		return PublicConfig{}, err
	}
	return cfg.Public(), nil
}

func (s *Service) Install(ctx context.Context) error { return s.installer.Ensure(ctx) }

func (s *Service) Logout(ctx context.Context) error {
	if _, err := s.runCLI(ctx, LogoutArgs(s.socket)...); err != nil {
		return fmt.Errorf("tailscale logout: %s", Scrub(err.Error()))
	}
	return nil
}

func (s *Service) Logs(n int) string {
	if s.proc == nil {
		return ""
	}
	return s.proc.LogTail(n)
}

func (s *Service) Status(ctx context.Context) FullStatus {
	out := FullStatus{
		Install:   s.installer.Status(),
		Preflight: s.preflight(ctx),
	}
	if cfg, err := s.store.Load(); err == nil {
		out.Config = cfg.Public()
	}
	if s.proc != nil {
		out.Running, _ = s.proc.IsRunning()
		out.LastError = s.proc.LastError()
	}
	// A LocalAPI failure is not a Status failure: a stopped daemon is a
	// normal state the UI must still render.
	if out.Running && s.local != nil {
		if node, err := s.local.Status(ctx); err == nil {
			out.Node = node
			out.PendingApproval = node.RoutesPendingApproval()
		}
	}
	return out
}
```

Add to `internal/tailscale/types.go`:

```go
// DefaultSocketPath is tailscaled's LocalAPI socket.
const DefaultSocketPath = "/opt/var/run/tailscale/tailscaled.sock"
```

Add to `internal/logging/types.go` next to the other `Sub*` constants:

```go
	SubTailscale      = "tailscale"
```

and re-export it from the tailscale package for readability:

```go
// SubTailscale is the logging subsystem tag for this package.
const SubTailscale = logging.SubTailscale
```

- [ ] **Step 5: Publish state transitions on the events bus**

Design spec §10 requires the UI to learn about state changes without polling
the heavy endpoints. Add to `internal/events/types.go`, beside the existing
event structs:

```go
// TailscaleStateEvent reports a backend-state transition of the tailscale
// node. Never carries an auth key or any node identity beyond the hostname.
type TailscaleStateEvent struct {
	BackendState    string `json:"backendState"`
	Running         bool   `json:"running"`
	SelfIP          string `json:"selfIp,omitempty"`
	PendingApproval bool   `json:"pendingApproval"`
}
```

Add the `Bus *events.Bus` field to `tailscale.Deps` and `Service`, and publish
only on change (a 5 s poll that republishes an unchanged state would wake
every subscriber for nothing):

```go
// publishState emits only on transition; lastPublished guards the repeat.
func (s *Service) publishState(st FullStatus) {
	if s.bus == nil {
		return
	}
	ev := events.TailscaleStateEvent{
		BackendState:    st.Node.BackendState,
		Running:         st.Running,
		SelfIP:          st.Node.SelfIP,
		PendingApproval: st.PendingApproval,
	}
	s.mu.Lock()
	unchanged := s.lastPublished == ev
	s.lastPublished = ev
	s.mu.Unlock()
	if !unchanged {
		s.bus.Publish("tailscale_state", ev)
	}
}
```

Call it at the end of `Status`, and add the test:

```go
func TestService_PublishStateOnlyEmitsOnChange(t *testing.T) {
	svc, _ := newTestService(t)
	bus := events.NewBus()
	svc.bus = bus
	_, ch, cancel := bus.Subscribe()
	defer cancel()

	st := FullStatus{Running: true, Node: Status{BackendState: "Running"}}
	svc.publishState(st)
	svc.publishState(st) // identical — must not emit again

	<-ch // first event
	select {
	case ev := <-ch:
		t.Errorf("unchanged state republished: %+v", ev)
	default:
	}
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/tailscale/... ./internal/logging/... ./internal/events/... -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/tailscale/service.go internal/tailscale/watchdog.go internal/tailscale/service_test.go internal/tailscale/watchdog_test.go internal/logging/types.go internal/events/types.go
git commit -m "feat(tailscale): service orchestration, watchdog and state events"
```

---

### Task 11: HTTP API handlers

**Files:**
- Create: `internal/api/tailscale.go`
- Test: `internal/api/tailscale_test.go`
- Modify: `internal/server/server_routes.go` (route table, next to the wdtt block around line 415)
- Modify: `internal/server/server.go` (add `tailscaleService api.TailscaleService` field + `deps` wiring, near line 91/241)

**Interfaces:**
- Consumes: `*tailscale.Service` methods from Task 10 through a local `TailscaleService` interface (the api package must not depend on the concrete type — same style as `api.WdttService`).
- Produces: `type TailscaleService interface`, `type TailscaleHandler`, `NewTailscaleHandler(TailscaleService) *TailscaleHandler`, and handler methods `GetStatus`, `GetConfig`, `UpdateConfig`, `Up`, `Down`, `Logout`, `Install`, `GetLogs`.

- [ ] **Step 1: Write the failing test**

```go
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hoaxisr/awg-manager/internal/tailscale"
)

type fakeTailscaleService struct {
	cfg        tailscale.PublicConfig
	lastAuthKey string
	updateErr  error
	loginURL   string
}

func (f *fakeTailscaleService) Status(context.Context) tailscale.FullStatus {
	return tailscale.FullStatus{Config: f.cfg, Running: true}
}
func (f *fakeTailscaleService) GetConfig() (tailscale.PublicConfig, error) { return f.cfg, nil }
func (f *fakeTailscaleService) UpdateConfig(_ context.Context, in tailscale.PublicConfig, authKey string) error {
	if f.updateErr != nil {
		return f.updateErr
	}
	f.cfg = in
	f.lastAuthKey = authKey
	return nil
}
func (f *fakeTailscaleService) Up(context.Context) (string, error) { return f.loginURL, nil }
func (f *fakeTailscaleService) Down(context.Context) error         { return nil }
func (f *fakeTailscaleService) Logout(context.Context) error       { return nil }
func (f *fakeTailscaleService) Install(context.Context) error      { return nil }
func (f *fakeTailscaleService) Logs(int) string                    { return "line one\nline two" }

func TestTailscaleHandler_GetStatusReturnsJSON(t *testing.T) {
	h := NewTailscaleHandler(&fakeTailscaleService{})
	rec := httptest.NewRecorder()
	h.GetStatus(rec, httptest.NewRequest(http.MethodGet, "/api/tailscale/status", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, body %s", rec.Code, rec.Body)
	}
	var out tailscale.FullStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !out.Running {
		t.Error("Running lost in serialization")
	}
}

func TestTailscaleHandler_UpdateConfigPassesAuthKeySeparately(t *testing.T) {
	svc := &fakeTailscaleService{}
	h := NewTailscaleHandler(svc)
	body := `{"config":{"egressMode":"wan","tunnelFallback":"drop","advertiseRoutes":[]},"authKey":"tskey-auth-xyz"}`
	rec := httptest.NewRecorder()
	h.UpdateConfig(rec, httptest.NewRequest(http.MethodPost, "/api/tailscale/config", strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, body %s", rec.Code, rec.Body)
	}
	if svc.lastAuthKey != "tskey-auth-xyz" {
		t.Errorf("authKey = %q", svc.lastAuthKey)
	}
}

func TestTailscaleHandler_UpdateConfigResponseNeverEchoesTheKey(t *testing.T) {
	h := NewTailscaleHandler(&fakeTailscaleService{})
	body := `{"config":{"egressMode":"wan","tunnelFallback":"drop","advertiseRoutes":[]},"authKey":"tskey-auth-xyz"}`
	rec := httptest.NewRecorder()
	h.UpdateConfig(rec, httptest.NewRequest(http.MethodPost, "/api/tailscale/config", strings.NewReader(body)))
	if strings.Contains(rec.Body.String(), "tskey-auth-xyz") {
		t.Errorf("auth key echoed back to the client: %s", rec.Body)
	}
}

func TestTailscaleHandler_UpReturnsLoginURL(t *testing.T) {
	h := NewTailscaleHandler(&fakeTailscaleService{loginURL: "https://login.tailscale.com/a/abc"})
	rec := httptest.NewRecorder()
	h.Up(rec, httptest.NewRequest(http.MethodPost, "/api/tailscale/up", nil))
	if !strings.Contains(rec.Body.String(), "https://login.tailscale.com/a/abc") {
		t.Errorf("login URL missing: %s", rec.Body)
	}
}

func TestTailscaleHandler_RejectsWrongMethod(t *testing.T) {
	h := NewTailscaleHandler(&fakeTailscaleService{})
	rec := httptest.NewRecorder()
	h.Up(rec, httptest.NewRequest(http.MethodGet, "/api/tailscale/up", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("code = %d, want 405", rec.Code)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -run TestTailscaleHandler -v`
Expected: FAIL — undefined `NewTailscaleHandler`.

- [ ] **Step 3: Write minimal implementation**

Follow the conventions in `internal/api/wdtt.go` for response helpers (`internal/api/response` / `api_common.go` — read one existing handler first and match it rather than inventing a new envelope).

```go
package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/hoaxisr/awg-manager/internal/tailscale"
)

// TailscaleService is the api package's view of *tailscale.Service, kept as
// an interface so the handlers are testable without the concrete type.
type TailscaleService interface {
	Status(ctx context.Context) tailscale.FullStatus
	GetConfig() (tailscale.PublicConfig, error)
	UpdateConfig(ctx context.Context, cfg tailscale.PublicConfig, authKey string) error
	Up(ctx context.Context) (string, error)
	Down(ctx context.Context) error
	Logout(ctx context.Context) error
	Install(ctx context.Context) error
	Logs(n int) string
}

type TailscaleHandler struct{ svc TailscaleService }

func NewTailscaleHandler(svc TailscaleService) *TailscaleHandler {
	return &TailscaleHandler{svc: svc}
}

func (h *TailscaleHandler) GetStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.svc.Status(r.Context()))
}

func (h *TailscaleHandler) GetConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := h.svc.GetConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

// updateConfigRequest keeps the secret out of PublicConfig: the config object
// the UI round-trips never carries a key, and the key travels in its own
// field that is never echoed back.
type updateConfigRequest struct {
	Config  tailscale.PublicConfig `json:"config"`
	AuthKey string                 `json:"authKey"`
}

func (h *TailscaleHandler) UpdateConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, errMethodNotAllowed)
		return
	}
	var req updateConfigRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := h.svc.UpdateConfig(r.Context(), req.Config, req.AuthKey); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	cfg, err := h.svc.GetConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

func (h *TailscaleHandler) Up(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, errMethodNotAllowed)
		return
	}
	url, err := h.svc.Up(r.Context())
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"loginUrl": url})
}

func (h *TailscaleHandler) Down(w http.ResponseWriter, r *http.Request) {
	h.simplePost(w, r, h.svc.Down)
}

func (h *TailscaleHandler) Logout(w http.ResponseWriter, r *http.Request) {
	h.simplePost(w, r, h.svc.Logout)
}

func (h *TailscaleHandler) Install(w http.ResponseWriter, r *http.Request) {
	h.simplePost(w, r, h.svc.Install)
}

func (h *TailscaleHandler) GetLogs(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"logs": h.svc.Logs(200)})
}

func (h *TailscaleHandler) simplePost(w http.ResponseWriter, r *http.Request, fn func(context.Context) error) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, errMethodNotAllowed)
		return
	}
	if err := fn(r.Context()); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
```

If `writeJSON` / `writeError` / `errMethodNotAllowed` do not exist under those exact names in `internal/api`, use whatever the neighbouring handlers use (`internal/api/api_common.go`) — do not add a parallel helper set.

- [ ] **Step 4: Register the routes**

In `internal/server/server_routes.go`, next to the wdtt block:

```go
	h.tailscaleHandler = api.NewTailscaleHandler(s.tailscaleService)
```

and in the mux section:

```go
	mux.HandleFunc("/api/tailscale/status", h.guarded(h.tailscaleHandler.GetStatus))
	mux.HandleFunc("/api/tailscale/config", h.guarded(h.tailscaleHandler.GetConfig))
	mux.HandleFunc("/api/tailscale/config/update", h.guarded(h.tailscaleHandler.UpdateConfig))
	mux.HandleFunc("/api/tailscale/up", h.guarded(h.tailscaleHandler.Up))
	mux.HandleFunc("/api/tailscale/down", h.guarded(h.tailscaleHandler.Down))
	mux.HandleFunc("/api/tailscale/logout", h.guarded(h.tailscaleHandler.Logout))
	mux.HandleFunc("/api/tailscale/install", h.guarded(h.tailscaleHandler.Install))
	mux.HandleFunc("/api/tailscale/logs", h.guarded(h.tailscaleHandler.GetLogs))
```

Add the `tailscaleHandler *api.TailscaleHandler` field to the handler struct (around `server_routes.go:35`) and `tailscaleService api.TailscaleService` to `Server` + `deps` (around `server.go:91` and `:241`).

- [ ] **Step 5: Run tests and build**

Run: `go test ./internal/api/... ./internal/server/... -v && go build ./...`
Expected: PASS and a clean build.

- [ ] **Step 6: Commit**

```bash
git add internal/api/tailscale.go internal/api/tailscale_test.go internal/server/server_routes.go internal/server/server.go
git commit -m "feat(tailscale): HTTP API and route registration"
```

---

### Task 12: Application wiring and autostart

**Files:**
- Create: `cmd/awg-manager/wiring_tailscale.go`
- Test: `cmd/awg-manager/wiring_tailscale_test.go`
- Modify: `cmd/awg-manager/boot.go` (call `setupTailscale()` next to `setupSingbox()`)

**Interfaces:**
- Consumes: `tailscale.New`, `tailscale.NewStore`, `tailscale.NewInstaller`, `tailscale.NewLocalAPI`, `Preflight`, `TunDevicePresent` (Tasks 1-10); `detectArch()` (`cmd/awg-manager/sysenv.go:23`); the existing downloader adapter used by `setupSingbox`.
- Produces: `func (a *app) setupTailscale()`, `func (a *app) tailscaleCLIRunner() tailscale.CLIRunner`, field `a.tailscaleService *tailscale.Service`.

- [ ] **Step 1: Write the failing test**

```go
package main

import (
	"context"
	"strings"
	"testing"

	"github.com/hoaxisr/awg-manager/internal/tailscale"
)

func TestTailscaleCLIRunner_ScrubsSecretsFromErrors(t *testing.T) {
	run := cliRunnerFor("/nonexistent/tailscale")
	_, err := run(context.Background(), "--socket=/tmp/x.sock", "up", "--authkey=tskey-auth-SECRET")
	if err == nil {
		t.Fatal("expected an error running a missing binary")
	}
	if strings.Contains(err.Error(), "SECRET") {
		t.Errorf("secret leaked through the runner error: %v", err)
	}
}

func TestProductionPreflightDeps_UsesRealTunProbe(t *testing.T) {
	deps := productionPreflightDeps(func() bool { return false }, func(context.Context) (int, bool) { return 0, false })
	res := tailscale.Preflight(context.Background(), deps)
	// On a dev machine /dev/net/tun is normally absent; the point is that the
	// probe is wired at all, not what it returns here.
	_ = res
	if deps.TunPresent == nil || deps.FreeBytes == nil || deps.RequiredBytes == nil {
		t.Fatal("production preflight deps are incompletely wired")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/awg-manager/ -run "TestTailscaleCLIRunner|TestProductionPreflight" -v`
Expected: FAIL — undefined `cliRunnerFor`.

- [ ] **Step 3: Write minimal implementation**

```go
package main

import (
	"context"
	"path/filepath"

	"github.com/hoaxisr/awg-manager/internal/sys/exec"
	"github.com/hoaxisr/awg-manager/internal/sys/osdetect"
	"github.com/hoaxisr/awg-manager/internal/sys/routerinfo"
	"github.com/hoaxisr/awg-manager/internal/tailscale"
)

const tailscaleDir = "/opt/etc/awg-manager/tailscale"

// cliRunnerFor returns a CLIRunner over the given tailscale binary. Both the
// output and any error text are scrubbed: `tailscale up` echoes its own argv
// on failure, which includes --authkey.
func cliRunnerFor(bin string) tailscale.CLIRunner {
	return func(ctx context.Context, args ...string) (string, error) {
		res, err := exec.Run(ctx, bin, args...)
		out := ""
		if res != nil {
			out = res.Stdout + res.Stderr
		}
		if err != nil {
			return tailscale.Scrub(out), errScrubbed(exec.FormatError(res, err))
		}
		return tailscale.Scrub(out), nil
	}
}

type scrubbedError struct{ msg string }

func (e *scrubbedError) Error() string { return e.msg }

func errScrubbed(err error) error {
	if err == nil {
		return nil
	}
	return &scrubbedError{msg: tailscale.Scrub(err.Error())}
}

// productionPreflightDeps wires the real environment probes. The two
// parameters are injected because they depend on services constructed
// elsewhere in the app graph.
func productionPreflightDeps(isOS5 func() bool, freeSlot func(context.Context) (int, bool)) tailscale.PreflightDeps {
	return tailscale.PreflightDeps{
		TunPresent:    tailscale.TunDevicePresent,
		FreeBytes:     func() (int64, bool) { return routerinfo.FreeBytes(tailscaleDir) },
		RequiredBytes: func() int64 { return 40 << 20 },
		ForeignDaemonPID: func(ctx context.Context) int {
			return foreignTailscaledPID(ctx, filepath.Join(tailscaleDir, "tailscaled"))
		},
		PortInUse:        func(ctx context.Context, port int) bool { return udpPortInUse(ctx, port) },
		FreeOpkgTunIndex: freeSlot,
		IsOS5:            isOS5,
	}
}

// setupTailscale builds the tailscale service and starts its workers.
func (a *app) setupTailscale() {
	installer := tailscale.NewInstaller(detectArch(), tailscaleDir, a.downloaderAdapter())
	store := tailscale.NewStore(a.dataDir)
	deps := tailscale.Deps{
		Store:     store,
		Installer: installer,
		LocalAPI:  tailscale.NewLocalAPI(tailscale.DefaultSocketPath),
		RunCLI:    cliRunnerFor(installer.CLIPath()),
		IsOS5:     osdetect.Is5,
		AppLogger: a.loggingService,
	}
	deps.Preflight = func(ctx context.Context) tailscale.PreflightResult {
		return tailscale.Preflight(ctx, productionPreflightDeps(osdetect.Is5, a.freeOpkgTunIndex))
	}
	a.tailscaleService = tailscale.New(deps)
}
```

Implement `foreignTailscaledPID` (scan `/proc/*/exe` for a `tailscaled` that is NOT our managed path — reuse the approach in `internal/singbox/installer/proc_sweep.go`), `udpPortInUse` (parse `/proc/net/udp`), and `a.freeOpkgTunIndex` (delegate to the same `LiveOpkgTunIndices` source wdtt uses). Read those existing implementations and mirror them rather than writing new probes.

Autostart: in `boot.go`, after `setupTailscale()`, start workers only when `cfg.Enabled && !cfg.ManuallyStopped`, and delay the first start by the same 8 s grace `wdtt` uses (`internal/wdtt/nat_reconcile.go:31`) so NDMS has settled.

- [ ] **Step 4: Run tests and build**

Run: `go test ./cmd/... -v && go build ./...`
Expected: PASS and a clean build.

- [ ] **Step 5: Commit**

```bash
git add cmd/awg-manager/wiring_tailscale.go cmd/awg-manager/wiring_tailscale_test.go cmd/awg-manager/boot.go
git commit -m "feat(tailscale): application wiring and autostart"
```

---

### Task 13: Frontend page

**Files:**
- Create: `frontend/src/routes/tailscale/+page.svelte`
- Create: `frontend/src/lib/api/tailscale.ts`
- Modify: the navigation component that lists `/singbox`, `/wdtt` etc. (find it with `grep -rn "wdtt" frontend/src/lib/components | head`)

**Interfaces:**
- Consumes: the eight `/api/tailscale/*` endpoints from Task 11.
- Produces: `getStatus()`, `updateConfig()`, `up()`, `down()`, `logout()`, `install()`, `getLogs()` in `tailscale.ts`.

- [ ] **Step 1: Read two existing pages first**

Read `frontend/src/routes/wdtt/+page.svelte` and `frontend/src/routes/singbox/+page.svelte`. Match their store usage, polling interval, banner components and i18n conventions. Do not introduce a new UI pattern for this page.

- [ ] **Step 2: Write the API client**

`frontend/src/lib/api/tailscale.ts` mirroring the existing api modules' fetch wrapper, exporting one function per endpoint with typed responses matching `tailscale.FullStatus` / `PublicConfig`.

- [ ] **Step 3: Build the page with these states**

The page must render each of these distinctly — they are the states the backend can actually report:

1. **arch unsupported** (`install.archSupported === false`) — explain, offer nothing.
2. **not installed / update available** — install button with progress.
3. **preflight blocked** (`preflight.ok === false`) — list every `blocker.message`; no enable button.
4. **needs login** (`node.backendState === "NeedsLogin"`) — the login URL as a link plus a QR, and an auth-key input as the alternative.
5. **running** — self IP/hostname, peer table (hostname, IP, online dot, `relay === "" ? "direct" : relay`, rx/tx, last handshake).
6. **routes pending approval** (`pendingApproval === true`) — a warning banner stating the routes are advertised but not approved in the admin console, with a link to it. This must be visually distinct from "running" or users will believe a broken setup is working.
7. **stopped** — start button.

Config form: hostname, login server (advanced), advertised routes (prefilled from the detected LAN prefix, editable), exit-node toggle, egress selector (WAN | tunnel picker), tunnel-down fallback (drop | bypass) shown only for tunnel egress, auth-key input (`type="password"`, placeholder indicating a stored key exists via `hasAuthKey`, empty means unchanged).

Log pane reading `/api/tailscale/logs`.

- [ ] **Step 4: Verify the build and lint**

Run: `cd frontend && npm run build && npm run lint`
Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/routes/tailscale frontend/src/lib/api/tailscale.ts frontend/src/lib/components
git commit -m "feat(tailscale): web UI"
```

---

### Task 14: Documentation and full verification

**Files:**
- Modify: `CHANGELOG.md`
- Modify: `README.md` (the "Возможности" list)
- Modify: `openapi.md` (the eight new endpoints)

- [ ] **Step 1: Add the CHANGELOG entry**

Match the existing entry style (Russian, with the issue/PR link format used by neighbouring entries). State plainly: tailscale support with subnet routing and exit node, egress selectable between WAN and a managed tunnel, requires the router's TUN device, MagicDNS is not resolved on the router itself.

- [ ] **Step 2: Update README and openapi.md**

One bullet in "Возможности"; the endpoint table in `openapi.md` following its existing format.

- [ ] **Step 3: Run the full verification suite**

```bash
go build ./... && go test ./... && (cd frontend && npm run build && npm run lint)
```

Expected: all green. Paste the actual output into the task review — a claim of "tests pass" without the output is not acceptable.

- [ ] **Step 4: Cross-compile check for every shipped arch**

```bash
GOOS=linux GOARCH=mipsle go build ./... && \
GOOS=linux GOARCH=mips go build ./... && \
GOOS=linux GOARCH=arm64 go build ./...
```

Expected: all three succeed.

- [ ] **Step 5: Commit**

```bash
git add CHANGELOG.md README.md openapi.md
git commit -m "docs(tailscale): changelog, readme and API docs"
```

---

## Deferred to a follow-up plan

Explicitly NOT built here, per design spec §14: `--accept-routes`, Tailscale SSH, Funnel/Serve, MagicDNS on the router, IPv6 exit-node egress, per-peer ACL editing.

## Known plan risks

1. **Task 0 gates Tasks 7 and 10.** If tailscaled will not attach to an NDMS-pre-created device, the interface call order changes (Task 7 Step 5 covers it). If the upstream mipsel build does not run on kernel 3.4, Task 2's URLs change to a self-built fork and the release script grows a build step.
2. **`PinnedVersion = "1.90.8"` is a placeholder** until Task 2 Step 5 fills in real checksums from the actual mirrored release. Verify the version exists upstream before generating.
3. **The `RouteOps` interface must be satisfied by the existing client-route operator** in `internal/tunnel/ops`. Confirm the concrete type's method set matches at Task 9; if the operator's methods are unexported, add a thin adapter in `cmd/awg-manager/tailscale_adapters.go` rather than widening the ops package's API.
