# stunmesh-go

STUNMESH is a Wireguard helper tool to get through Full-Cone NAT.

Inspired by manuels' [wireguard-p2p](https://github.com/manuels/wireguard-p2p) project

Supported Platform:
- Linux (amd64, arm, arm64, mips)
- MacOS* (amd64, arm64)
- FreeBSD (amd64, arm64)

> [!NOTE]
> We only support wireguard-go in MacOS, Wireguard App store version is not supported  because of sandbox currently.

Tested with
- VyOS 2025.07.14-0022-rolling (built-in Wireguard kernel module)
- Ubuntu with Wireguard in Kernel module
- MacOS Wireguard-go 0.0.20230223, Wireguard-tools 1.0.20210914
- FreeBSD 14.3-RELEASE (built-in Wireguard)
- OPNSense 25.1 (built-in Wireguard)

## Implementation

Use raw socket and cBPF filter to send and receive STUN 5389's packet to get public ip and port with same port of wireguard interface.<br />
Encrypt public info with Curve25519 sealedbox and save it using configured storage plugins.<br />
stunmesh-go will create and update records with domain `<sha1 in hex>.<subdomain>.<your_domain>` (or `<sha1 in hex>.<your_domain>` if no subdomain configured).<br />
Once getting info from internet, it will setup peer endpoint with wireguard tools.<br />

✅ **Plugin system supported** - Multiple storage backends with flexible configuration - supports exec plugin for custom implementations

## Limitatoin

In FreeBSD, MacOS (BSD-based system), we will listen on the default route interface for STUN response message by default. If your system is without default route, it will fail.

Planning to assign specific or all interfaces in future release.

## Build

```bash
make all
```

> [!NOTE]
> For FreeBSD and MacOS, please use GNU Makefile `gmake` to build.

## Usage

```
sudo ./stunmesh-go
```

Or use STUNMESH-go in the container

```
docker pull tjjh89017/stunmesh
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
        plugin: cloudflare1
  wg1:
    peers:
      "<PEER_NAME>":
        public_key: "<PUBLIC_KEY_IN_BASE64>"
        plugin: cloudflare2
      "<PEER_NAME>":
        public_key: "<PUBLIC_KEY_IN_BASE64>"
        plugin: exec_plugin1
stun:
  address: "stun.l.google.com:19302"
plugins:
  cloudflare1:
    type: cloudflare
    zone_name: "example.com"
    subdomain: "wg"
    api_token: "${CLOUDFLARE_API_TOKEN}"
  cloudflare2:
    type: cloudflare
    zone_name: "example.com"
    api_token: "${CLOUDFLARE2_API_TOKEN}"
  exec_plugin1:
    type: exec
    command: "python3"
    args: ["/path/to/script.py", "--config", "/path/to/config"]
```

### Plugin System

stunmesh-go now supports a flexible plugin system that allows you to:

- **Multiple storage backends**: Use different storage solutions for different peers
- **Named plugin instances**: Configure multiple instances of the same plugin type
- **Per-peer plugin assignment**: Each peer can use a different plugin instance

#### Supported Plugin Types

**Cloudflare DNS Plugin (`type: cloudflare`)**
- Stores peer information in Cloudflare DNS TXT records
- Configuration:
  - `zone_name`: Your domain name managed by Cloudflare
  - `subdomain`: Subdomain prefix for DNS records (optional, defaults to empty)
  - `api_token`: Cloudflare API token with DNS edit permissions

**Exec Plugin (`type: exec`)**
- Executes external scripts/programs for storage operations
- Configuration:
  - `command`: Command to execute
  - `args`: Command line arguments (optional)
- Protocol: JSON over stdin/stdout

#### Exec Plugin Protocol

The exec plugin communicates with external programs using JSON over stdin/stdout. Your program should:

1. Read JSON request from stdin
2. Process the request (GET or SET operation)
3. Write JSON response to stdout
4. Exit with code 0 for success, non-zero for error

**Request Format:**
```json
{
  "operation": "get|set",
  "key": "peer_identifier_string",
  "value": "encrypted_data_for_set_operation"
}
```

**Response Format:**
```json
{
  "success": true,
  "value": "encrypted_data_for_get_operation",
  "error": "error_message_if_failed"
}
```

#### Exec Plugin Examples

**Python Example (`/usr/local/bin/stunmesh-storage.py`):**
```python
#!/usr/bin/env python3
import json
import sys
import os

# Simple file-based storage
STORAGE_DIR = "/var/lib/stunmesh"

def ensure_storage_dir():
    os.makedirs(STORAGE_DIR, exist_ok=True)

def get_value(key):
    file_path = os.path.join(STORAGE_DIR, f"{key}.txt")
    try:
        with open(file_path, 'r') as f:
            return f.read().strip()
    except FileNotFoundError:
        return None

def set_value(key, value):
    ensure_storage_dir()
    file_path = os.path.join(STORAGE_DIR, f"{key}.txt")
    with open(file_path, 'w') as f:
        f.write(value)

def main():
    try:
        # Read JSON request from stdin
        request = json.load(sys.stdin)
        
        operation = request.get("operation")
        key = request.get("key")
        
        if operation == "get":
            value = get_value(key)
            if value is not None:
                response = {"success": True, "value": value}
            else:
                response = {"success": False, "error": "Key not found"}
                
        elif operation == "set":
            value = request.get("value")
            set_value(key, value)
            response = {"success": True}
            
        else:
            response = {"success": False, "error": "Unknown operation"}
            
        # Write JSON response to stdout
        json.dump(response, sys.stdout)
        
    except Exception as e:
        response = {"success": False, "error": str(e)}
        json.dump(response, sys.stdout)
        sys.exit(1)

if __name__ == "__main__":
    main()
```

**Bash Example (`/usr/local/bin/stunmesh-storage.sh`):**
```bash
#!/bin/bash

STORAGE_DIR="/var/lib/stunmesh"
mkdir -p "$STORAGE_DIR"

# Read JSON from stdin
INPUT=$(cat)

# Parse JSON using jq
OPERATION=$(echo "$INPUT" | jq -r '.operation')
KEY=$(echo "$INPUT" | jq -r '.key')

case "$OPERATION" in
    "get")
        FILE_PATH="$STORAGE_DIR/${KEY}.txt"
        if [ -f "$FILE_PATH" ]; then
            VALUE=$(cat "$FILE_PATH")
            echo "{\"success\": true, \"value\": \"$VALUE\"}"
        else
            echo "{\"success\": false, \"error\": \"Key not found\"}"
        fi
        ;;
    "set")
        VALUE=$(echo "$INPUT" | jq -r '.value')
        FILE_PATH="$STORAGE_DIR/${KEY}.txt"
        echo "$VALUE" > "$FILE_PATH"
        echo "{\"success\": true}"
        ;;
    *)
        echo "{\"success\": false, \"error\": \"Unknown operation\"}"
        exit 1
        ;;
esac
```

**Redis Example (`/usr/local/bin/stunmesh-redis.py`):**
```python
#!/usr/bin/env python3
import json
import sys
import redis

# Redis connection
r = redis.Redis(host='localhost', port=6379, db=0, decode_responses=True)

def main():
    try:
        request = json.load(sys.stdin)
        
        operation = request.get("operation")
        key = f"stunmesh:{request.get('key')}"
        
        if operation == "get":
            value = r.get(key)
            if value is not None:
                response = {"success": True, "value": value}
            else:
                response = {"success": False, "error": "Key not found"}
                
        elif operation == "set":
            value = request.get("value")
            r.set(key, value)
            # Optional: Set expiration (e.g., 24 hours)
            r.expire(key, 86400)
            response = {"success": True}
            
        else:
            response = {"success": False, "error": "Unknown operation"}
            
        json.dump(response, sys.stdout)
        
    except Exception as e:
        response = {"success": False, "error": str(e)}
        json.dump(response, sys.stdout)
        sys.exit(1)

if __name__ == "__main__":
    main()
```

**Configuration Examples:**
```yaml
plugins:
  file_storage:
    type: exec
    command: "/usr/local/bin/stunmesh-storage.py"
    
  bash_storage:
    type: exec
    command: "/usr/local/bin/stunmesh-storage.sh"
    
  redis_storage:
    type: exec
    command: "python3"
    args: ["/usr/local/bin/stunmesh-redis.py"]
    
  remote_api:
    type: exec
    command: "curl"
    args: ["-s", "-X", "POST", "-H", "Content-Type: application/json", "--data-binary", "@-", "https://api.example.com/stunmesh"]
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
        plugin: cloudflare_main
stun:
  address: "stun.l.google.com:19302"
plugins:
  cloudflare_main:
    type: cloudflare
    zone_name: "<ZONE_NAME>"
    api_token: "<API_TOKEN>"
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
        plugin: cloudflare_main
stun:
  address: "stun.l.google.com:19302"
plugins:
  cloudflare_main:
    type: cloudflare
    zone_name: "<ZONE_NAME>"
    api_token: "<API_TOKEN>"
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
        plugin: cloudflare_main
stun:
  address: "stun.l.google.com:19302"
plugins:
  cloudflare_main:
    type: cloudflare
    zone_name: "<ZONE_NAME>"
    api_token: "<API_TOKEN>"
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
        plugin: cloudflare_main
stun:
  address: "stun.l.google.com:19302"
plugins:
  cloudflare_main:
    type: cloudflare
    zone_name: "<ZONE_NAME>"
    api_token: "<API_TOKEN>"
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

## License
This program is free software; you can redistribute it and/or modify it under the terms of the GNU General Public License as published by the Free Software Foundation; either version 2 of the License, or (at your option) any later version.
