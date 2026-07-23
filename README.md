# stunmesh-go

STUNMESH is a WireGuard helper tool that establishes peer-to-peer connections through NAT — without any self-hosted coordination infrastructure.

It discovers each node's public IP and port via STUN, encrypts the endpoint with a Curve25519 sealed box, and shares it through a pluggable storage backend (Cloudflare DNS, OpenDHT, or your own script). Every node reads its peers' endpoints from the same storage and configures WireGuard accordingly. No rendezvous server, no relay, no VPS in the middle.

Inspired by manuels' [wireguard-p2p](https://github.com/manuels/wireguard-p2p) project.

**📖 Full documentation: [docs.stunmesh.dev](https://docs.stunmesh.dev)**

## Talks & Presentations

- [FOSDEM 2026 - STUNMESH-go: Building P2P WireGuard Mesh Without Self-Hosted Infrastructure](https://fosdem.org/2026/schedule/event/YQWEDC-stunmesh-go_building_p2p_wireguard_mesh_without_self-hosted_infrastructure/)
- [Slides](https://speakerdeck.com/tjjh89017/fosdem-2026-stunmesh-go-building-p2p-wireguard-mesh-without-self-hosted-infrastructure)
- [Recording Video](https://video.fosdem.org/2026/h1302/YQWEDC-stunmesh-go_building_p2p_wireguard_mesh_without_self-hosted_infrastructure.av1.webm)

## NAT Type Support

- ✅ **Full Cone NAT**, **Restricted Cone NAT**, **Port Restricted Cone NAT**: fully supported
- ⚠️ **Symmetric NAT**: may be difficult to support due to unpredictable port mapping

For best results, ensure at least one peer is behind a cone NAT type.

## Supported Platforms

- **Linux** (amd64, arm, arm64, mipsle) - Normal and UPX-compressed binaries
- **macOS** (amd64, arm64) - Normal binaries only
- **FreeBSD** (amd64, arm64) - Normal binaries only, requires `wireguard-tools`

> [!IMPORTANT]
> FreeBSD binaries use the `wgcli` backend, which invokes `wg(8)`. The base system ships the
> `if_wg` kernel module but not that tool, so `pkg install wireguard-tools` is required.
> See [Backend Selection](https://docs.stunmesh.dev/reference/build#backend-selection).

> [!NOTE]
> We only support wireguard-go in MacOS, Wireguard App store version is not supported because of sandbox currently.

## Quick Start

Download a binary from the [releases page](https://github.com/tjjh89017/stunmesh-go/releases), or use the container image:

```bash
docker pull tjjh89017/stunmesh
```

stunmesh-go needs raw socket access, so run it as root next to an already-configured WireGuard interface:

```bash
sudo ./stunmesh-go
```

It runs as a daemon by default; pass `-oneshot` to publish and establish 3 times and then exit.

Configuration is read from `/etc/stunmesh/config.yaml`, `~/.stunmesh/config.yaml`, or `./config.yaml` (`.yml` also works), or pass a file directly with `-c <file>`. A minimal two-node setup with the built-in Cloudflare plugin:

```yaml
---
refresh_interval: "1m"
log:
  level: "info"
interfaces:
  wg0:
    peers:
      "PEER_B":
        public_key: "<PEER_B_PUBLIC_KEY_BASE64>"
        plugin: cf
stun:
  addresses: ["stun.l.google.com:19302"]
plugins:
  cf:
    type: builtin
    name: cloudflare
    zone: example.com
    token: "<CLOUDFLARE_API_TOKEN>"
    subdomain: wg
```

Run the same setup on the other node (with this node's public key), wait roughly two refresh intervals, and the tunnel comes up. Verify with `wg show` or by pinging the peer's tunnel address.

> [!IMPORTANT]
> stunmesh-go reads the WireGuard device's state once at startup — restart it after the
> interface is recreated, the listen port or fwmark changes, or `config.yaml` is edited.
> Under systemd, bind it to the WireGuard unit (`After=` + `BindsTo=`) so this is enforced
> automatically. See [when stunmesh-go must be restarted](https://docs.stunmesh.dev/getting-started#when-stunmesh-go-must-be-restarted).

## Documentation

| Topic | Link |
|---|---|
| Getting started | [docs.stunmesh.dev/getting-started](https://docs.stunmesh.dev/getting-started) |
| Configuration reference (protocols, STUN servers, ping monitoring) | [docs.stunmesh.dev/configuration/overview](https://docs.stunmesh.dev/configuration/overview) |
| Storage plugins (built-in, exec, shell, dedup) | [docs.stunmesh.dev/plugins/overview](https://docs.stunmesh.dev/plugins/overview) |
| Writing your own plugin | [docs.stunmesh.dev/plugins/exec-protocol](https://docs.stunmesh.dev/plugins/exec-protocol) |
| Deployment guides (VyOS, macOS, OSPF/VRF) | [docs.stunmesh.dev/guides/vyos](https://docs.stunmesh.dev/guides/vyos) |
| Building from source & backend selection | [docs.stunmesh.dev/reference/build](https://docs.stunmesh.dev/reference/build) |
| Platform internals (fwmark, BPF capture, listen interfaces) | [docs.stunmesh.dev/reference/platform-internals](https://docs.stunmesh.dev/reference/platform-internals) |

## Build

```bash
make all
```

> [!NOTE]
> For FreeBSD and MacOS, please use GNU Makefile `gmake` to build.

Build options (built-in plugin selection, binary minimization, WireGuard backend) are documented at [docs.stunmesh.dev/reference/build](https://docs.stunmesh.dev/reference/build). Contrib plugins live in [`contrib/`](contrib/) and are built with `make plugin`.

## Future work / Roadmap

- auto execute when routing engine notify change

## License

This program is free software; you can redistribute it and/or modify it under the terms of the GNU General Public License as published by the Free Software Foundation; either version 2 of the License, or (at your option) any later version.
