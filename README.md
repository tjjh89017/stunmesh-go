# stunmesh-go
STUNMESH is a Wireguard helper tool to get through Full-Cone NAT.

Inspired by manuels' [wireguard-p2p](https://github.com/manuels/wireguard-p2p) project

Tested with UBNT ER-X v2.0.8-hotfix.1 and Wireguard v1.0.20210424

:warning: This PoC code is dirty and need refactor.

## Implement
Use raw socket and cBPF filter to send and receive STUN 5389's packet to get public ip and port with same port of wireguard interface.
Encrypt public info with Curve25519 sealedbox and save it into Cloudflare DNS TXT record.
stunmesh-go will create and update a record with domain "<sha1 in hex>.<your_domain>".
Once getting info from internet, it will setup peer endpoint with wireguard tools.

stunmesh-go assume you only have one peer per wireguard interface.

Still need refactor to get plugin support

## Build

### Build for UBNT ER-X

```./build-for-erx.sh```

### Build for Linux in native environment

```go build .```

## Usage
Please edit `start.sh` and execute it with root privileges.
You should use crontab to trigger stunmesh-go periodically to update Cloudflare TXT record and receive remote peer's public info

## Extra Usage
You could use OSPF on Wireguard interface to create full mesh site-to-site VPN with dynamic routing.
Never be bothered to setup static route.

### Dynamic Routing
Wireguard interface didn't have link status (link up, down)
OSPF will say hello to remote peer periodically to check peer status.
It will also check wireguard's link status is up or not.
You can also reduce hello and dead interval in OSPF to make rapid response
Please also make sure setup access list or route map in OSPF to prevent redistribute public ip to remote peer.
It might cause to get incorrect route to remote peer endpoint and fail connect remote peer if you have multi-node.

BGP will only update when route table is changed.
It will take longer time to determine link status.
Not suggest to use BFD with BGP when router is small scale.
It will take too much overhead for link status detection

### VRF
If you used this with your public network, and it's possible to enable VRF, please enable VRF with Wireguard interface.
Once you need Wireguard interface or private network to access internet.
Try to use VRF leaking to setup another default route to internet

## License
This program is free software; you can redistribute it and/or modify it under the terms of the GNU General Public License as published by the Free Software Foundation; either version 2 of the License, or (at your option) any later version.
