# OpenDHT Plugin (Shell Version)

The same OpenDHT storage as [`contrib/opendht`](../opendht/), reimplemented
for systems without `jq`: POSIX `sh`, `sed`, `base64` and `curl` **or** `wget`
are enough. A default OpenWrt image (busybox + `uclient-fetch` as `wget`)
qualifies as-is.

It speaks the same wire format as the built-in and exec opendht plugins —
same envelope, same magic — so shell nodes interoperate with nodes running
either of those.

## Requirements

- POSIX shell, `sed`, `base64` (all in busybox)
- `curl`, or any `wget` that accepts `--post-data` (GNU wget, busybox wget,
  OpenWrt's `uclient-fetch`)
- HTTPS endpoints need TLS support in the fetcher and a CA store
  (`ca-bundle` on OpenWrt, included by default since 21.02)
- At least one OpenDHT proxy endpoint. There is no default; see
  [which proxies to use](../opendht/README.md#which-proxies-to-use)

## Configuration

```yaml
plugins:
  opendht:
    type: shell
    command: /usr/local/bin/stunmesh-opendht-shell
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
| `-magic` | `stunmesh-v1` | Envelope tag used to recognise our own values. Restricted to `A-Za-z0-9._-` |
| `-timeout` | `15` | Per-request timeout in seconds |

## `dedup` must stay false

OpenDHT values expire after 10 minutes; the mesh only stays reachable because
every refresh cycle republishes the endpoint. `dedup: true` skips exactly the
publish that keeps the value alive. See the
[full explanation](../opendht/README.md#dedup-must-stay-false).

## Notes

Everything else — which proxies to trust, running your own, expiry, security
model, performance — is identical to the exec version and documented once in
[`contrib/opendht/README.md`](../opendht/README.md).
