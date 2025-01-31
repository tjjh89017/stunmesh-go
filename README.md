# stunmesh-go

STUNMESH is a Wireguard helper tool to get through Full-Cone NAT.

Inspired by manuels' [wireguard-p2p](https://github.com/manuels/wireguard-p2p) project

Tested with
- VyOS 1.5-rolling-202501180006 (built-in Wireguard kernel module)
- Ubuntu with Wireguard in Kernel module
- MacOS Wireguard-go 0.0.20230223, Wireguard-tools 1.0.20210914

## Implementation

Use raw socket and cBPF filter to send and receive STUN 5389's packet to get public ip and port with same port of wireguard interface.<br />
Encrypt public info with Curve25519 sealedbox and save it into Cloudflare DNS TXT record.<br />
stunmesh-go will create and update a record with domain `<sha1 in hex>.<your_domain>`.<br />
Once getting info from internet, it will setup peer endpoint with wireguard tools.<br />

stunmesh-go assume you only have one peer per wireguard interface.

:warning: Still need refactor to get plugin support

## Build

```bash
make all
```

## Usage

```
sudo ./stunmesh-go
```

### Configuration

Put the configuration below paths:

* `/etc/stunmesh/config.yaml`
* `~/.stunmesh/config.yaml`
* `./config.yaml`

```yaml
---
refresh_interval: "1m"
log:
  level: "debug"
interfaces:
  wg0:
    peers:
      "<PEER_NAME>":
        public_key: "<PUBLIC_KEY_IN_BASE64>"
  wg1:
    peers:
      "<PEER_NAME>":
        public_key: "<PUBLIC_KEY_IN_BASE64>"
      "<PEER_NAME>":
        public_key: "<PUBLIC_KEY_IN_BASE64>"
stun:
  address: "stun.l.google.com:19302"
cloudflare:
  api_token: "<API_TOKEN>"
  zone_name: "<ZONE_NAME>"
```

## Example Usage

These are the strict examples to show you how to use STUNMESH-go in your environment. You can mix the setup in your envrionments.

For example, you can use VyOS router with Mac with STUNMESH-go to connect Wireguard tunnel with only Mobile networks or Public IPs.

### Example 1

Suppose you have two VyOS routers with LTE modem on them.

Hardware in Site A:
1. VyOS_A with LTE modem and SIM card.
2. Other devices under the subnet.

Hardware in Site B:
1. VyOS_B with LTE modem and SIM card.
2. Other devices under the subnet.

#### Steps in Site A

1. Configure VyOS_A with LTE connections.
2. Configure VyOS_A with the following commands.
3. Wait for it connected. (Mostly, it will require 2 times of refresh_interval)

VyOS Commands
```bash
mkdir -p /config/user-data/stunmesh
cat <<EOF > /config/user-data/stunmesh/config.yaml
refresh_interval: "1m"
log:
  level: "debug"
interfaces:
  wg0:
    peers:
      "VYOS_B":
        public_key: "<VYOS_B_PUBLIC_KEY>"
stun:
  address: "stun.l.google.com:19302"
cloudflare:
  api_token: "<API_TOKEN>"
  zone_name: "<ZONE_NAME>"
EOF

configure
set container name stunmesh allow-host-networks
set container name stunmesh capability 'net-admin'
set container name stunmesh capability 'net-raw'
set container name stunmesh capability 'net-bind-service'
set container name stunmesh capability 'sys-admin'
set container name stunmesh image 'tjjh89017/stunmesh'
set container name stunmesh uid '0'
set container name stunmesh volume certs destination '/etc/ssl/certs'
set container name stunmesh volume certs mode 'ro'
set container name stunmesh volume certs source '/etc/ssl/certs'
set container name stunmesh volume config destination '/etc/stunmesh'
set container name stunmesh volume config mode 'ro'
set container name stunmesh volume config source '/config/user-data/stunmesh'
set interfaces wireguard wg0 address '192.168.10.1/24'
set interfaces wireguard wg0 port '<YOUR_WIREGUARD_PORT>'
set interfaces wireguard wg0 ip adjust-mss '1380'
set interfaces wireguard wg0 ipv6 adjust-mss '1360'
set interfaces wireguard wg0 mtu '1420'
set interfaces wireguard wg0 peer VYOS_B allowed-ips '192.168.10.2/24'
set interfaces wireguard wg0 peer VYOS_B persistent-keepalive '15'
set interfaces wireguard wg0 peer VYOS_B public-key <VYOS_B_PUBLIC_KEY>
set interfaces wireguard wg0 private-key <VYOS_A_PRIVATE_KEY>

# You will need to setup firewall rules to allow ingress traffic to '<YOUR_WIREGUARD_PORT>'
# Please check the VyOS docs to use nft style firewall or Zone Based Firewall
commit
save
```

#### Steps in Site B

1. Configure VyOS_B with LTE connections.
2. Configure VyOS_B with the following commands.
3. Wait for it connected. (Mostly, it will require 2 times of refresh_interval)

VyOS Commands
```bash
mkdir -p /config/user-data/stunmesh
cat <<EOF > /config/user-data/stunmesh/config.yaml
refresh_interval: "1m"
log:
  level: "debug"
interfaces:
  wg0:
    peers:
      "VYOS_A":
        public_key: "<VYOS_A_PUBLIC_KEY>"
stun:
  address: "stun.l.google.com:19302"
cloudflare:
  api_token: "<API_TOKEN>"
  zone_name: "<ZONE_NAME>"
EOF

configure
set container name stunmesh allow-host-networks
set container name stunmesh capability 'net-admin'
set container name stunmesh capability 'net-raw'
set container name stunmesh capability 'net-bind-service'
set container name stunmesh capability 'sys-admin'
set container name stunmesh image 'tjjh89017/stunmesh'
set container name stunmesh uid '0'
set container name stunmesh volume certs destination '/etc/ssl/certs'
set container name stunmesh volume certs mode 'ro'
set container name stunmesh volume certs source '/etc/ssl/certs'
set container name stunmesh volume config destination '/etc/stunmesh'
set container name stunmesh volume config mode 'ro'
set container name stunmesh volume config source '/config/user-data/stunmesh'
set interfaces wireguard wg0 address '192.168.10.2/24'
set interfaces wireguard wg0 port '<YOUR_WIREGUARD_PORT>'
set interfaces wireguard wg0 ip adjust-mss '1380'
set interfaces wireguard wg0 ipv6 adjust-mss '1360'
set interfaces wireguard wg0 mtu '1420'
set interfaces wireguard wg0 peer VYOS_A allowed-ips '192.168.10.2/24'
set interfaces wireguard wg0 peer VYOS_A persistent-keepalive '15'
set interfaces wireguard wg0 peer VYOS_A public-key <VYOS_A_PUBLIC_KEY>
set interfaces wireguard wg0 private-key <VYOS_B_PRIVATE_KEY>

# You will need to setup firewall rules to allow ingress traffic to '<YOUR_WIREGUARD_PORT>'
# Please check the VyOS docs to use nft style firewall or Zone Based Firewall
commit
save
```

#### Verify the Wireguard connections is established

Ping each other with Wireguard interface's IP to test the connection

#### Extra Configuration

You may need to configure some static route or dynamic route to connect two subnets with different sites.

### Example 2

Suppose you have two LTE/5G routers and two downlink MacOS or Linux computers. Here we use the following setup to demo.

Hardware in Site A:
1. Netgear M5 5G router (with Asia Pacific Telecom, Now Far EasTone Telecom)
2. MacOS Intel-based `Intel Mac`

Hardware in Site B:
1. iPhone 15 Pro (with Chunghwa Telecom 4G LTE SIM)
2. Macbook Air M3 `Mac M3`

#### Steps in Site A

1. Connect your `Intel Mac` with Netgear M5 to get the internet.
2. Install wireguard-go and wireguard-tools in your Mac.
3. Download STUNMESH-go for your Mac's architecture to `/tmp/stunmesh-go`.
4. Prepare your wireguard configuration as below
5. Prepare `config.yaml` as below, please fill your `utunX` interface from the result of `wg-quick`
6. Run wireguard tunnel with `wg-quick up /tmp/wg0.conf`
7. Run STUNMESH-go in `/tmp/`, `cd /tmp; sudo ./stunmesh-go`
8. Wait for it connected. (Mostly, it will require 2 times of `refresh_interval`)

Wiregaurd Config `/tmp/wg0.conf`
```
[Interface]
PrivateKey = <INTEL_MAC_PRIVATE_KEY>
Address = 192.168.10.1/24

[Peer]
PublicKey = <MAC_M3_PUBLIC_KEY>
AllowedIPs = 192.168.10.0/24
PersistentKeepalive = 25
```

STUNMESH-go `/tmp/config.yaml`

```yaml
---
refresh_interval: "1m"
log:
  level: "debug"
interfaces:
  "<utunX>":
    peers:
      "MAC_M3":
        public_key: "<MAC_M3_PUBLIC_KEY>"
stun:
  address: "stun.l.google.com:19302"
cloudflare:
  api_token: "<API_TOKEN>"
  zone_name: "<ZONE_NAME>"
```

#### Steps in Site B

1. Connect your `Mac M3` with your iPhone15 to get the internet.
2. Install wireguard-go and wireguard-tools in your Mac.
3. Download STUNMESH-go for your Mac's architecture to `/tmp/stunmesh-go`.
4. Prepare your wireguard configuration as below
5. Prepare `config.yaml` as below, please fill your `utunX` interface from the result of `wg-quick`
6. Run wireguard tunnel with `wg-quick up /tmp/wg0.conf`
7. Run STUNMESH-go in `/tmp/`, `cd /tmp; sudo ./stunmesh-go`
8. Wait for it connected. (Mostly, it will require 2 times of `refresh_interval`)

Wiregaurd Config `/tmp/wg0.conf`
```
[Interface]
PrivateKey = <MAC_M3_PRIVATE_KEY>
Address = 192.168.10.2/24

[Peer]
PublicKey = <INTEL_MAC_PUBLIC_KEY>
AllowedIPs = 192.168.10.0/24
PersistentKeepalive = 25
```

STUNMESH-go `/tmp/config.yaml`

```yaml
---
refresh_interval: "1m"
log:
  level: "debug"
interfaces:
  "<utunX>":
    peers:
      "INTEL_MAC":
        public_key: "<INTE_MAC_PUBLIC_KEY>"
stun:
  address: "stun.l.google.com:19302"
cloudflare:
  api_token: "<API_TOKEN>"
  zone_name: "<ZONE_NAME>"
```

#### Verify the Wireguard connections is established

Ping each other to check or you can use `wg` to show the info.



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

## Future work / Roadmap

- one shot command
- auto execute when routing engine notify change
- plugin based storage

## License
This program is free software; you can redistribute it and/or modify it under the terms of the GNU General Public License as published by the Free Software Foundation; either version 2 of the License, or (at your option) any later version.
