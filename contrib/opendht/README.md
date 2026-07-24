# OpenDHT Plugin

Stores peer endpoint data in the [OpenDHT](https://github.com/savoirfairelinux/opendht)
distributed hash table through an OpenDHT proxy server's REST API.

Unlike the other plugins, this one needs no account, no API token and no
quota: OpenDHT is a public Kademlia network with no operator. The trade-off
is that nobody guarantees your data stays there — see [Limitations](#limitations)
before deploying this.

## Requirements

- `curl`
- `jq`
- At least one OpenDHT proxy endpoint. There is no default; see
  [Which proxies to use](#which-proxies-to-use).

Tested on Linux and macOS.

## Configuration

```yaml
plugins:
  opendht:
    type: exec
    command: /usr/local/bin/stunmesh-opendht
    args:
      - "-endpoint"
      - "https://dhtproxy2.jami.net"
      - "-endpoint"
      - "https://dhtproxy3.jami.net"
    dedup: false

interfaces:
  wg0:
    peers:
      peer1:
        public_key: "base64_encoded_key"
        plugin: opendht
```

### Options

| Option | Default | Description |
|---|---|---|
| `-endpoint` | none, required | OpenDHT proxy base URL, with scheme. Repeat to add fallbacks |
| `-magic` | `stunmesh-v1` | Envelope tag used to recognise our own values |
| `-timeout` | `15` | Per-request timeout in seconds |

Do not lower `-timeout` much. A DHT lookup that finds nothing legitimately
takes ~6 seconds to converge; a short timeout turns a slow success into a
false "not found".

### Which proxies to use

There is no built-in default. Which proxy you trust is a decision about whose
infrastructure your mesh depends on, so the plugin makes you name one rather
than inheriting a choice silently.

Savoir-faire Linux runs several public proxies for Jami. Measured July 2026,
in the order suggested above:

| Endpoint | Notes |
|---|---|
| `https://dhtproxy2.jami.net` | Reachable over IPv4 and IPv6. Hosted in Europe |
| `https://dhtproxy3.jami.net` | Reachable over IPv4 and IPv6. Hosted in Canada, so it is the better first choice from the Americas |
| `https://dhtproxy.jami.net` | **Avoid.** See below |
| `https://dhtproxy1.jami.net` | **Avoid.** IPv6 only in practice |

Order them by whichever is closest to you; the plugin tries them in the order
given and only moves on when a request fails. Listing two from different
regions costs nothing until the first one breaks.

### Why not `dhtproxy.jami.net`

It is the name the Jami documentation points at, but it is a rotation over two
addresses and one of them does not serve:

| Address | TCP 80 | TCP 443 | Reverse DNS |
|---|---|---|---|
| `141.94.96.2` | answers | answers | `dhtproxy1.jami.net` |
| `141.94.96.254` | refused | refused | none |

`141.94.96.254` is what `dhtproxy1.jami.net` resolves to today, and it appeared
in roughly nine of ten lookups across four resolvers. The AAAA record is
healthy, so a dual-stack host prefers IPv6 and never notices. An IPv4-only host
draws a single A record per query with nothing to fall back to, so it fails
outright — intermittently, which is far more confusing than a clean failure:

```
exec plugin error: set request failed: curl: (7) Failed to connect to
dhtproxy.jami.net port 443 after 299 ms: Couldn't connect to server
```

Naming `dhtproxy2` and `dhtproxy3` explicitly sidesteps the whole thing.

## `dedup` must stay false

**Never set `dedup: true` for this plugin.**

OpenDHT values expire after 10 minutes (`DEFAULT_VALUE_EXPIRATION` in
`opendht/value.h`). The mesh only stays reachable because every refresh cycle
republishes the endpoint before the previous value expires. `dedup: true`
skips the publish when the endpoint is unchanged — which is exactly when it
must not be skipped, since nothing else refreshes the value. The mesh would
keep working for up to 10 minutes and then quietly stop.

This is the case the "expiring/TTL backends" warning in the root `CLAUDE.md`
is about.

## How it works

`Get`/`Set` map onto the proxy's REST API:

```
POST /key/<sha1-hex>   {"data":"<base64>"}     -> store
GET  /key/<sha1-hex>                           -> newline-delimited value objects
```

An OpenDHT key is an `InfoHash`: 160 bits, i.e. 40 hex characters. stunmesh
keys are SHA1 hex, so they are used as-is with no transformation.

A key holds a *set* of values, not a single overwritable slot. There is no
overwrite and no delete: the proxy answers `DELETE` with `501 Not Implemented`,
and re-publishing under the same key adds another value rather than replacing
the previous one (supplying a fixed `id` does not change this — unsigned
values have no owner, so nothing identifies them as versions of each other).
Values only go away when they expire.

That matters in normal operation, not just under attack. Publishing every 5
minutes against a 10-minute expiry means a key always holds two or three of
*our own* values at once, and the proxy does not return them in chronological
order. The stored payload is therefore wrapped in an envelope:

```json
{"magic": "stunmesh-v1", "ts": 1752700000, "data": "<hex ciphertext>"}
```

`Get` decodes every value under the key, keeps the ones carrying our magic,
and returns the most recent by `ts`. Values that are not our envelope — or
not JSON at all — are ignored. The `ts` sort is load-bearing: without it a
peer would pick an arbitrary one of its own recent endpoints.

## Limitations

**Listing several endpoints does not make the DHT more available.** They are
all entrances to the same DHT, so the fallbacks help when a proxy is down, not
when a value has expired. If nothing published within the last 10 minutes, no
endpoint will have it.

**The endpoints are somebody else's infrastructure.** The `jami.net` proxies
are run by Savoir-faire Linux for the Jami messenger. They publish no terms of
service, no SLA and no rate limits for third-party use. They may start refusing
or throttling stunmesh traffic at any time, with no recourse. The dead address
behind `dhtproxy.jami.net` is what that looks like in practice: a broken record
nobody announces, fixes on your schedule, or answers questions about. If that
matters to you, run your own proxy (see below).

**IPv4-only hosts should check their endpoints.** NAT traversal is an IPv4
problem, so the peers that most need stunmesh are also the ones least likely to
have IPv6 — and a proxy that is IPv6-only, as `dhtproxy1.jami.net` is today,
looks fine to everyone testing from a dual-stack machine. Verify with `curl -4`
before relying on one:

```bash
curl -4 -sS https://dhtproxy2.jami.net/node/info | jq '.ipv4.good'
```

**The network is small.** `GET /node/info` on the public proxy reports a
`network_size_estimation` of roughly 4096 IPv4 nodes — this is essentially
Jami's online user base, not a BitTorrent-sized swarm. Your value lives on
the handful of nodes closest to your key, and those are consumer machines
that come and go.

**Lookups are slow.** Measured against `dhtproxy.jami.net`: a `set` takes
~1.9s, a `get` that hits ~2.3s, and a `get` that misses ~6s. This is inherent
to a DHT lookup, not a fault of the proxy — but it is two orders of magnitude
slower than an HTTP KV store, and `Establish` performs one `get` per peer.

**Anyone can publish under your key.** Keys are derived from public peer
identifiers, so they are not secret. The magic envelope filters out noise and
accidental collisions; it is not a security boundary. Someone who deliberately
publishes a value with our magic and a future `ts` will win the `sort_by(.ts)`
and break that peer until they stop. The payload stays confidential regardless
— it is NaCl-encrypted before it reaches this plugin — so the exposure is
denial of service, not disclosure. OpenDHT's `putSigned` would close this gap
at the cost of managing a second keypair; this plugin does not use it.

## Running your own proxy

The surest way not to depend on somebody else's uptime. Run a node with the
proxy interface enabled and point `-endpoint` at it:

```bash
docker run -d -i --name dhtnode -p 8080:8080 \
  ghcr.io/savoirfairelinux/opendht/opendht \
  dhtnode -b bootstrap.jami.net:4222 --proxyserver 8080
```

```yaml
args: ["-endpoint", "http://127.0.0.1:8080"]
```

Both flags matter, and getting either wrong fails quietly:

- **`-i`** keeps stdin open. `dhtnode` runs an interactive command loop, so
  without it the container reads EOF and exits `0` immediately — a clean exit
  that does not look like a failure.
- **`-b`** names a bootstrap node. `dhtnode` does not bootstrap on its own:
  without it the node never joins the DHT (`.ipv4.good` stays at 0) and yet
  `set` and `get` still appear to succeed, because values land in the node's
  local storage and it reads them straight back. Nothing else on the network
  can see them.

Check readiness before trusting it:

```bash
curl -sS http://127.0.0.1:8080/node/info | jq '.ipv4.good'
```

`good` must be greater than zero. Do not gate on
`network_size_estimation` — it is still `null` at `good=2`, when the node is
already usable. Bootstrapping takes a few seconds.

Note that `brew install opendht` does not help here: the formula builds with
`-DOPENDHT_C=ON -DOPENDHT_TOOLS=ON` only, and `OPENDHT_PROXY_SERVER` defaults
to `OFF`, so the resulting `dhtnode` has no proxy interface. Building it in
means also supplying Restinio and jsoncpp.

Running your own proxy changes who runs the HTTP endpoint, not which DHT the
data lives in — the node still joins the public OpenDHT network. It needs
outbound UDP, which some restrictive NATs block.
