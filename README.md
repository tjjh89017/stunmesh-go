# stunmesh-go
STUNMESH is a Wireguard helper tool to get through Full-Cone NAT.

Inspired by manuels' [wireguard-p2p](https://github.com/manuels/wireguard-p2p) project

Tested with UBNT ER-X v2.0.8-hotfix.1 and Wireguard v1.0.20210424

:warning: This PoC code is dirty and need refactor.

## Implement
Use raw socket and cBPF filter to send and receive STUN 5389's packet to get public ip and port with same port of wireguard interface.<br />
Encrypt public info with Curve25519 sealedbox and save it into Cloudflare DNS TXT record.<br />
stunmesh-go will create and update a record with domain `<sha1 in hex>.<your_domain>`.<br />
Once getting info from internet, it will setup peer endpoint with wireguard tools.<br />

stunmesh-go assume you only have one peer per wireguard interface.

Still need refactor to get plugin support

## Build

### Build for UBNT ER-X
```
./build-for-erx.sh
```

### Build for Linux in native environment
```
go build .
```

## Usage
Please edit `start.sh` and execute it with root privileges.<br />
You should use crontab to trigger stunmesh-go periodically to update Cloudflare TXT record and receive remote peer's public info. <br />

### Configuration

Put the configuration below paths:

* `/etc/stunmesh/config.toml`
* `~/.stunmesh/config.toml`
* `./config.toml`

```toml
wg = "wg03"

[cloudflare]
api_key = "<Your API Key>"
api_email = "<Your email>"
zone_name = "<Your Domain>"
```

> The environment variables is higher priority than the configuration file.

## Extra Usage
You could use OSPF on Wireguard interface to create full mesh site-to-site VPN with dynamic routing.<br />
Never be bothered to setup static route.<br />

### Dynamic Routing
Wireguard interface didn't have link status (link up, down)<br />
OSPF will say hello to remote peer periodically to check peer status.<br />
It will also check wireguard's link status is up or not.<br />
You can also reduce hello and dead interval in OSPF to make rapid response<br />
Please also make sure setup access list or route map in OSPF to prevent redistribute public ip to remote peer.<br />
It might cause to get incorrect route to remote peer endpoint and fail connect remote peer if you have multi-node.<br />

BGP will only update when route table is changed.<br />
It will take longer time to determine link status.<br />
Not suggest to use BFD with BGP when router is small scale.<br />
It will take too much overhead for link status detection<br />

### VRF
If you used this with your public network, and it's possible to enable VRF, please enable VRF with Wireguard interface.<br />
Once you need Wireguard interface or private network to access internet.<br />
Try to use VRF leaking to setup another default route to internet<br />

## Example config

Wireguard in Edgerouter
```
    wireguard wg03 {
        address <some route peer to peer IP>/30
        description "to lab"
        ip {
            ospf {
                network point-to-point
            }
        }
        listen-port <wg port>
        mtu 1420
        peer <Remote Peer Public Key> {
            allowed-ips 0.0.0.0/0
            allowed-ips 224.0.0.5/32
            persistent-keepalive 15
        }
        private-key ****************
        route-allowed-ips false
    }
```

OSPF in Edgerouter
```
policy {
    access-list 1 {
        description OSPF
        rule 1 {
            action permit
            source {
                inverse-mask 0.0.0.255
                network <Your LAN CIDR>
            }
        }
        rule 99 {
            action deny
            source {
                any
            }
        }
    }
}

protocols {
    ospf {
        access-list 1 {
            export connected
        }
        area 0.0.0.0 {
            network <Your network CIDR>
        }
        parameters {
            abr-type cisco
            router-id <Router ID>
        }
        passive-interface default
        passive-interface-exclude <Your WG interface>
        redistribute {
            connected {
                metric-type 2
            }
        }
    }
}
```

in stunmesh-go start.sh
```
#!/bin/bash

export CF_API_KEY=<Your API Key>
export CF_API_EMAIL=<Your email>
export CF_ZONE_NAME=<Your Domain>

wgs=("wg02" "wg03")

for wg in ${wgs[@]}
do
        echo $wg
        export WG=$wg
        /tmp/stunmesh-go
        sleep 10
        /tmp/stunmesh-go
done
```

## Future work / Roadmap

- daemon and one shot command
- auto execute when routing engine notify change
- plugin based storage
- config file via JSON or other format

## License
This program is free software; you can redistribute it and/or modify it under the terms of the GNU General Public License as published by the Free Software Foundation; either version 2 of the License, or (at your option) any later version.
