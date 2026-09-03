# Architecture

This document records the shape decisions of this codebase that aren't
obvious from reading a single file — the recurring patterns, and why they
are the way they are. It is not a tour of every package; see `CLAUDE.md`
for that. Update it when a pattern here changes or a new one recurs enough
to be worth naming.

## Style declaration

Each internal subsystem defines narrow, consumer-owned interfaces for its
external dependencies and generates mocks for them — see
`internal/ctrl/repository.go` (`DeviceRepository`/`PeerRepository`),
`internal/ctrl/stun.go` (`StunResolver`), and `internal/ctrl/endpoint.go`
(`EndpointEncryptor`/`EndpointDecryptor`) for the canonical shape: the
interface is declared next to the code that calls it, sized to only the
methods that caller needs, with a `//go:generate mockgen` directive on top
and a `wire.Bind` in `wire.go` where the implementation lives in another
package. New cross-package dependencies should follow this shape unless
there's a stated reason not to.

## Patterns

### `FilterPeerService`-style orchestration in `entity`

- **When to apply**: a small orchestration step that combines two or more
  repository/provider calls into one operation, where the result is still
  just entities (no new domain concept, no side effects beyond what the
  collaborators already do).
- **Shape**: a struct in `internal/entity` (e.g. `FilterPeerService`,
  `internal/entity/filter_peer.go`) that depends on narrow interfaces it
  declares itself (`ConfigPeerProvider`, `DevicePeerChecker`), constructed
  via `New...` and exposing a single `Execute` method. Implementations of
  those interfaces live in other packages (e.g. `internal/repo.Peers`
  implements `DevicePeerChecker`, `config.DeviceConfig` implements
  `ConfigPeerProvider`) and are wired with `wire.Bind` in `wire.go`.
- **Forces it resolves**: the orchestration needs both config-sourced peer
  data (with plugin/protocol info) and live WireGuard device state, and
  callers (`BootstrapController`) only want the merged, filtered result.
  Putting this in `entity` avoids a `usecase`/`service` package for a
  single instance of the pattern, and keeps the entity package as the
  place that understands what "an existing peer" means.
- **Revisit if**: a third orchestration service like `FilterPeerService` is
  added outside `entity`, or an orchestration step starts needing anything
  beyond entities and repository-shaped calls (multi-step transactions,
  eventing, retries) — re-open the placement question at that point rather
  than growing `entity` into a general dumping ground.

### Alias-not-shared-type for cross-package identifiers

- **When to apply**: two packages need to talk about the same underlying
  value (a WireGuard public key) but must not import each other, or a
  package wants to satisfy another package's interface without importing
  that package.
- **Shape**: `internal/wg/api.go` defines `type Key = [32]byte` and
  `internal/wgproxy/demux.go` defines `type PeerKey = [32]byte` — both true
  Go alias declarations (`=`), so a `wg.Key` and a `wgproxy.PeerKey` are the
  same type as each other and as `[32]byte`, interchangeable without
  conversion. Separately, `internal/stun.Resolver.Resolve` has the same
  method signature as `internal/ctrl.StunResolver` without `stun` importing
  `ctrl` — `ctrl` depends on the structural match, not on a shared
  interface type declared in `stun`.
- **Forces it resolves**: `internal/wg`, `internal/wgproxy`, and
  `internal/stun` sit at the bottom of the dependency graph and must not
  import `internal/ctrl` or each other just to share a key type or satisfy
  an interface; a distinct named type per package (`wg.Key`, `wgproxy.PeerKey`
  as separate struct-like types) would force conversions at every call site
  and an import to get the target type in scope. Aliasing to `[32]byte` and
  relying on structural typing sidesteps both without an import cycle.
- **Revisit if**: the key ever needs behavior beyond being 32 bytes
  (validation, a `String()` method with different formatting per package)
  — a plain alias can't carry that, and the pattern would need to become a
  real named type with an explicit conversion at the package boundary.

### Proxy-mode wiring lives in the `app` composition root

- **When to apply**: n/a as a pattern to reuse — this entry documents why
  `app/proxy_stack.go` is where it is, so it isn't "fixed" into an internal
  package by a future cleanup pass.
- **Shape**: `proxyStack` (in `app/proxy_stack.go`, package `app`) bundles
  the `wg.Client` and `*stun.Resolver` whose construction depends on
  whether proxy mode is on, and `newProxyStack` builds one or the other
  unconditionally so `wire_gen.go` stays a single code path with no
  build-tag or runtime branching inside the generated file.
- **Forces it resolves**: the choice of which concrete `wg.Client`/
  `stun.Resolver` to construct depends on `config.Config`,
  `config.DeviceConfig`, and `runtime.GOOS` (proxy mode is always on for
  Windows) — inputs that only exist once wiring is assembled in `app`.
  Moving this into an `internal/` package would mean that package importing
  both `internal/wg` and `internal/stun` purely to make a construction-time
  choice that `wire_gen.go` already needs to call into anyway; keeping it
  in `app` keeps that choice next to the rest of the composition root
  instead of adding a package whose only job is picking between two
  already-existing constructors. `app` sits outside `internal/` because it
  is itself the importable embedding surface (`app.New`/`App.Run`/
  `App.Close`) — an external program embeds the daemon by importing `app`,
  which `internal/` visibility rules would forbid.
- **Revisit if**: proxy-mode selection logic grows beyond picking a
  constructor (e.g. needs its own tests that don't want to run against the
  full `app` build, or a caller outside `app` needs the same
  decision) — at that point extracting `proxyModeEnabledForGOOS` and
  friends into an internal package pays for itself.

### Plugin registry: process-global `init()`-time self-registration

- **When to apply**: n/a as a pattern to reuse elsewhere in this codebase —
  documents why `internal/plugin/registry` exists as its own package and
  why registration happens in `init()`.
- **Shape**: `internal/plugin/registry/registry.go` holds a package-level
  `map[string]Factory` guarded by a `sync.RWMutex`, with `Register` called
  from each built-in plugin's own `init()` (e.g.
  `internal/plugin/builtin/cloudflare/cloudflare.go`), and `Get` used by
  `internal/plugin/manager.go` to look a factory up by name at plugin
  construction time. `internal/plugin/imports.go` blank-imports every
  built-in package unconditionally so their `init()` functions always run;
  build tags on each built-in's implementation/stub pair (see
  `CLAUDE.md`'s "Adding New Built-in Plugins") control whether that
  `init()` does anything.
- **Forces it resolves**: `plugin` (the manager) needs to create a built-in
  `Store` by name without every built-in's package needing to import
  `plugin`, and without `plugin` needing a hand-maintained list of built-ins
  to import. Splitting `registry` out as its own package breaks what would
  otherwise be a cycle (`plugin` importing each built-in, each built-in
  importing `plugin` to register itself) and lets `imports.go` be the only
  place that lists built-ins, with build tags — not a switch statement —
  deciding which ones actually register.
- **Revisit if**: registration needs to be anything other than "always run
  once at process start" (e.g. conditional or repeated registration, or
  registration that depends on config read at runtime) — `init()`-time
  self-registration assumes registration has no inputs beyond the build
  tags baked into the binary.
- **Lifecycle**: plugin instances live for the whole `App` lifetime — a
  plugin owning connections or goroutines implements `io.Closer` to release
  them. `plugin.Manager.Close` closes every instance that does so and runs
  from `App.Close` (via the plugin manager provider's cleanup func) and from
  the mobile controller's `stop`. `App.New` creates the App's own lifetime
  `context.Context` and passes it into the plugin manager provider (instead
  of `context.Background()`), so plugin construction is tied to the App
  rather than to whichever `Run` call happens to be active. `App.Close`
  cancels that context and waits for any in-flight `Run`/`RunOneshot` to
  return before releasing resources, so a plugin is never closed out from
  under a live worker.

### One escape hatch for every outbound path

- **Where**: `internal/plugin/dialer` (`Escape`, `WithEscape`, `DialContext`,
  `Resolver`, `ResolveAddrPort`), consumed by the built-in plugins' HTTP
  transport, by their hostname lookups, and by STUN discovery's server
  lookups on every platform — mobile's `controller.resolveSTUN`, and desktop's
  `stun.Connect` (socket-owning and proxy-backed alike), which read the escape
  `ctrl.PublishController.discoverEndpoints` puts in the discovery context.
- **Rule**: anything that opens a socket while stunmesh may be managing a
  covering tunnel goes through the dialer's escape — including name
  resolution, which is a socket too. The escape travels in the `context`
  rather than being fixed at construction, so one instance can serve peers on
  different interfaces.
- **Why**: a covering allowed-IPs route swallows the very call that would
  bring the tunnel up. Name resolution is the half that is easy to miss,
  because the standard library hides the socket: `net.Resolve*` and
  `net.DefaultResolver` reach the platform resolver, which on android is
  Bionic's `getaddrinfo` routing over whatever network is default — the
  tunnel itself, once up. A lookup needed to establish the tunnel then gets
  routed into it. On desktop the socket is unmarked and unbound instead, so a
  covering allowed-IPs route sends it the same way. `dialer.Resolver` forces
  the pure-Go resolver so its `Dial` hook is consulted; the hook applies the
  platform's escape and, where `Escape.DNSServers` is set (mobile only —
  desktop keeps its resolv.conf nameservers), aims at those servers.
- **Consequence**: low-level components do not resolve names. `mobilebind`'s
  `Discover` takes an already-resolved `netip.AddrPort` so it has nothing to
  resolve with, and the caller — which holds the escape — does the lookup.
  Desktop's `stun.Connect` still takes a `host:port` string (it is the
  `StunClient` contract, shared with the proxy-backed client), but resolves it
  through `dialer.ResolveAddrPort` rather than `net.ResolveUDPAddr`.
- **Revisit if**: a platform gains a resolver that can be pinned to a
  specific underlay network, which would make the forced pure-Go resolver
  and the explicit nameserver list unnecessary there.

## CI/e2e coverage narrowing

`.github/workflows/main.yml` runs three e2e layers gated behind `build`,
each covering fewer OS/arch cells than the last: `e2e` runs across 8 cells
(linux/darwin/freebsd/windows × amd64/arm64), `e2e-proxy` runs the
netns-based wgproxy relay test on a single deterministic host, and
`e2e-realnet-subject` (the real-NAT two-VM layer) runs one subject cell per
OS — 4 cells total, always paired with a Linux anchor. This narrowing is
deliberate, not an oversight: `build` is cheap (a cross-compile per
OS/arch, no runtime cost), `e2e` needs a real runner per cell so it's
already limited by GitHub-hosted runner availability (there is, for
example, no macOS 32-bit or FreeBSD-native runner), and `e2e-realnet` adds
a second VM and depends on outbound internet reachability, which makes it
the most expensive and least parallelizable layer. Narrowing arch coverage
at each layer — rather than either running every cell everywhere or
dropping a whole OS from realnet coverage — keeps the expensive layer
exercising each supported OS at least once without multiplying its cost by
the same 8x that `e2e` can afford. A one-line version of this rationale
already lives as a comment directly above the `e2e` job's matrix in
`main.yml`; this section is the expanded version for readers who land here
first. When adding a new OS/arch to the support matrix, add it to `build`
first, then decide separately (based on runner availability and realnet's
internet-dependency cost) whether it earns an `e2e` cell and a subject slot
in `e2e-realnet-subject` — it does not need to skip straight to all three.

## Resolved duplication (formerly tracked here)

The architecture review that produced this document originally called
for tracking three known-duplication gaps in this section: the desktop/
mobile endpoint-selection and STUN-discovery duplication, the mobile
plugin-traffic escape gap, and the Linux/Darwin-BSD STUN client
duplication. All three were resolved before this document was written —
desktop and mobile now share `ctrl.DiscoverEndpoints` (`internal/ctrl/discover.go`)
and `ctrl.SelectEndpoint` (`internal/ctrl/endpoint_select.go`); mobile
plugin HTTP traffic is protected from full-tunnel routing (commit
`baf40a8`, corrected in `38b9a7f`; the DNS resolution half of that same
escape was closed later, in `ebb467e`); and the Linux/Darwin-BSD STUN client
duplication was collapsed into `internal/stun/helper_socket.go` (commit
`ed4ae03`). Nothing is presently tracked as known, unresolved duplication;
this section is left in place as a marker for where such a register would
live if one is needed again — check `git log` for the commits above if you
need the pre-fix shape.
