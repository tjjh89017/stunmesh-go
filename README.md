# stunmesh-go

STUNMESH is a Wireguard helper tool to establish peer-to-peer connections through NAT.

Inspired by manuels' [wireguard-p2p](https://github.com/manuels/wireguard-p2p) project

## NAT Type Support

stunmesh-go supports the following NAT types:

- ✅ **Full Cone NAT**: Fully supported
- ✅ **Restricted Cone NAT**: Fully supported
- ✅ **Port Restricted Cone NAT**: Fully supported
- ⚠️ **Symmetric NAT**: May be difficult to support due to unpredictable port mapping

For best results, ensure at least one peer is behind a cone NAT type.

## Supported Platforms

- **Linux** (amd64, arm, arm64, mipsle) - Normal and UPX-compressed binaries
- **macOS** (amd64, arm64) - Normal binaries only
- **FreeBSD** (amd64, arm64) - Normal binaries only

> [!NOTE]
> We only support wireguard-go in MacOS, Wireguard App store version is not supported because of sandbox currently.

## Tested With

- VyOS 2025.07.14-0022-rolling (built-in Wireguard kernel module)
- Ubuntu with Wireguard in Kernel module
- macOS Wireguard-go 0.0.20230223, Wireguard-tools 1.0.20210914
- FreeBSD 14.3-RELEASE (built-in Wireguard)
- OPNsense 25.1 (built-in Wireguard)
- EdgeRouter X (EdgeOS 3.0.0)

## Implementation

Use raw socket and cBPF filter to send and receive STUN 5389's packet to get public ip and port with same port of wireguard interface.<br />
Encrypt public info with Curve25519 sealedbox and save it using configured storage plugins.<br />
stunmesh-go will create and update records with domain `<sha1 in hex>.<subdomain>.<your_domain>` (or `<sha1 in hex>.<your_domain>` if no subdomain configured).<br />
Once getting info from internet, it will setup peer endpoint with wireguard tools.<br />

✅ **Plugin system supported** - Multiple storage backends with flexible configuration - supports exec plugin for custom implementations

## Platform Implementation Details

**Linux**: Uses raw sockets with BPF filtering to listen on all interfaces system-wide. No interface-specific limitations.

**FreeBSD and macOS (BSD-based systems)**: Uses BPF with interface-specific packet capture. stunmesh-go will listen on all eligible network interfaces for STUN response messages, excluding the specific WireGuard interface being managed. This provides better resilience for systems with multiple network paths or backup routes compared to single default route dependency.

## Build

```bash
make all
```

> [!NOTE]
> For FreeBSD and MacOS, please use GNU Makefile `gmake` to build.

### Build Options

The Makefile supports several options for customizing the build:

**Binary Minimization Options:**
- `STRIP=1` - Strip debug symbols from binary (reduces size)
- `TRIMPATH=1` - Remove file system paths from binary (improves reproducibility)
- `UPX=1` - Compress binary with UPX (requires UPX to be installed)
- `EXTRA_MIN=1` - Enable all minimization options above (STRIP + TRIMPATH + UPX)

**Built-in Plugin Options:**
- `BUILTIN=all` - (Default) Compile with all available built-in plugins (cloudflare)
- `BUILTIN=` - Build without any built-in plugins (minimal binary)
- `BUILTIN=builtin_cloudflare` - Compile with specific built-in Cloudflare plugin only
- `BUILTIN="builtin_cloudflare builtin_xxx"` - Multiple plugins (quote required, space-separated)

**Available Built-in Plugins:**
- `builtin_cloudflare` - Cloudflare DNS plugin for peer endpoint storage

**Build Examples:**
```bash
# Normal build (includes all built-in plugins by default)
make build

# Build without any built-in plugins (minimal binary)
make build BUILTIN=

# Build with stripped symbols
make build STRIP=1

# Build with all minimizations (strip, trimpath, and UPX compression)
make build EXTRA_MIN=1

# Clean and build with extra minimization (includes all built-ins by default)
make all EXTRA_MIN=1

# Build minimal binary without built-in plugins
make all BUILTIN= EXTRA_MIN=1

# Build with specific built-in plugin only
make all BUILTIN=builtin_cloudflare EXTRA_MIN=1
```

**Platform-Specific Notes:**
- CGO is automatically enabled for FreeBSD and OpenBSD (required for these platforms)
- CGO is disabled by default for Linux and Darwin (produces static binaries)
- UPX compression significantly reduces binary size but requires the `upx` tool to be installed

**Release Binaries:**
- **Linux**: Both normal and `-upx` suffix binaries are provided (built with `EXTRA_MIN=1`)
- **macOS**: Only normal binaries are provided (no UPX version)
- **FreeBSD**: Only normal binaries are provided (no UPX version)

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
    protocol: "ipv4"  # Optional: "ipv4" (default), "ipv6", or "dualstack" for STUN discovery
    peers:
      "<PEER_NAME>":
        public_key: "<PUBLIC_KEY_IN_BASE64>"
        plugin: cloudflare1
        protocol: "ipv4"  # Optional: peer protocol selection (default: "ipv4")
        ping:
          enabled: true
          target: "192.168.1.100"
  wg1:
    protocol: "dualstack"  # Discover both IPv4 and IPv6 endpoints
    peers:
      "<PEER_NAME>":
        public_key: "<PUBLIC_KEY_IN_BASE64>"
        plugin: cloudflare2
        protocol: "prefer_ipv6"  # Prefer IPv6, fallback to IPv4
        ping:
          enabled: true
          target: "10.0.0.50"
          interval: "60s"
          timeout: "10s"
      "<PEER_NAME>":
        public_key: "<PUBLIC_KEY_IN_BASE64>"
        plugin: exec_plugin1
        # protocol defaults to "ipv4" if not specified
        # ping configuration is completely optional
stun:
  address: "stun.l.google.com:19302"
ping_monitor:
  interval: "5s"
  timeout: "2s"
  fixed_retries: 3
plugins:
  cloudflare1:
    type: exec
    command: "/usr/local/bin/stunmesh-cloudflare"
    args: ["-zone", "example.com", "-token", "${CLOUDFLARE_API_TOKEN}", "-subdomain", "wg"]
  cloudflare2:
    type: exec
    command: "/usr/local/bin/stunmesh-cloudflare"
    args: ["-zone", "example.com", "-token", "${CLOUDFLARE2_API_TOKEN}"]
  exec_plugin1:
    type: exec
    command: "python3"
    args: ["/path/to/script.py", "--config", "/path/to/config"]
```

### Protocol Configuration

stunmesh-go supports both IPv4 and IPv6 for STUN discovery and peer connections. The protocol configuration operates at two levels:

#### Interface Protocol

Controls which STUN discovery protocols are used for the interface:

- `ipv4` (default): Use IPv4 STUN discovery only
- `ipv6`: Use IPv6 STUN discovery only
- `dualstack`: Perform both IPv4 and IPv6 STUN discovery

**Example:**
```yaml
interfaces:
  wg0:
    protocol: "ipv4"      # IPv4 only (default if not specified)
    peers: {...}
  wg1:
    protocol: "ipv6"      # IPv6 only
    peers: {...}
  wg2:
    protocol: "dualstack" # Both IPv4 and IPv6
    peers: {...}
```

#### Peer Protocol

Controls which endpoint a peer will use when establishing connections:

- `ipv4` (default): Use only IPv4 endpoint
- `ipv6`: Use only IPv6 endpoint
- `prefer_ipv4`: Prefer IPv4, fallback to IPv6 if IPv4 is unavailable
- `prefer_ipv6`: Prefer IPv6, fallback to IPv4 if IPv6 is unavailable

**Example:**
```yaml
interfaces:
  wg0:
    protocol: "dualstack"  # Publish both IPv4 and IPv6 endpoints
    peers:
      peer1:
        public_key: "<BASE64_KEY>"
        plugin: cloudflare1
        protocol: "ipv4"         # This peer will only use IPv4
      peer2:
        public_key: "<BASE64_KEY>"
        plugin: cloudflare1
        protocol: "prefer_ipv6"  # Prefer IPv6, fallback to IPv4
      peer3:
        public_key: "<BASE64_KEY>"
        plugin: cloudflare1
        # protocol not specified - defaults to "ipv4"
```

**Important Notes:**

- The interface protocol determines what endpoints are discovered and published
- The peer protocol determines which endpoint to use from the published data
- For `dualstack` interfaces, both IPv4 and IPv6 endpoints are stored, but each peer selects which to use
- STUN server must support the chosen protocol (not all STUN servers support IPv6)
- Network must have proper IPv6 configuration and routing for IPv6 to work
- Ping monitoring currently only supports IPv4

### Plugin System

stunmesh-go now supports a flexible plugin system that allows you to:

- **Multiple storage backends**: Use different storage solutions for different peers
- **Named plugin instances**: Configure multiple instances of the same plugin type
- **Per-peer plugin assignment**: Each peer can use a different plugin instance

#### Supported Plugin Types

**Built-in Plugin (`type: builtin`)** ⚡
- Compiled directly into the stunmesh-go binary using build tags
- Configuration:
  - `name`: Built-in plugin name (e.g., `cloudflare`)
  - Additional plugin-specific configuration fields
- Benefits:
  - **83% smaller deployment size** (2.2 MB vs 13.6 MB with external plugins)
  - Single binary deployment
  - No external plugin processes (reduced memory usage)
  - No IPC overhead
- Available built-in plugins: `cloudflare`
- Build with: `make all BUILTIN=builtin_cloudflare EXTRA_MIN=1`

**Exec Plugin (`type: exec`)**
- Executes external scripts/programs for storage operations
- Configuration:
  - `command`: Command to execute
  - `args`: Command line arguments (optional)
- Protocol: JSON over stdin/stdout
- Best for: Complex plugins requiring structured data handling

**Shell Plugin (`type: shell`)**
- Simplified plugin type for shell scripts
- Configuration:
  - `command`: Command to execute
  - `args`: Command line arguments (optional)
- Protocol: Shell variables over stdin, plain text over stdout
- Best for: Simple shell scripts without JSON parsing requirements

**Built-in Plugin Configuration Example:**
```yaml
plugins:
  cf_builtin:
    type: builtin
    name: cloudflare
    zone_name: example.com
    api_token: your_api_token_here
    subdomain: stunmesh  # Optional

interfaces:
  wg0:
    protocol: ipv4
    peers:
      peer1:
        public_key: "base64_encoded_key"
        plugin: cf_builtin
        protocol: ipv4
```

**Contrib Plugins**

Additional plugins are available in the [`contrib/`](contrib/) directory:
- **Cloudflare DNS Plugin**: Stores peer information in Cloudflare DNS TXT records
  - See [contrib/cloudflare/README.md](contrib/cloudflare/README.md) for setup instructions
- More community plugins can be added here

To use contrib plugins, build them and reference them as exec plugins in your configuration.

#### Exec Plugin Protocol

The exec plugin communicates with external programs using JSON over stdin/stdout. Your program should:

1. Read JSON request from stdin
2. Process the request (GET or SET operation)
3. Write JSON response to stdout
4. Exit with code 0 for success, non-zero for error

**Request Format:**
```json
{
  "action": "get|set",
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

**Stored Data Format:**

The `value` field contains encrypted endpoint data in hexadecimal format. The encryption/decryption process:

1. **Encryption** (`internal/crypto/endpoint.go:37`):
   - Plain JSON is encrypted using NaCl box (Curve25519 + XSalsa20 + Poly1305)
   - Result is hex-encoded: `hex.EncodeToString(nonce + encryptedData)`
   - Example: `"a1b2c3d4e5f6..."`

2. **Decryption** (`internal/crypto/endpoint.go:45`):
   - Hex string is decoded: `hex.DecodeString(value)`
   - NaCl box decrypts the data
   - Result is plain JSON

**Decrypted JSON Structure:**
```json
{
  "ipv4": "1.2.3.4:51820",
  "ipv6": "[2001:db8::1]:51820"
}
```

**Notes:**
- Plugins only store/retrieve the hex-encoded string; no encryption or JSON parsing needed
- Field presence depends on interface protocol configuration:
  - `ipv4` mode: Only `ipv4` field present
  - `ipv6` mode: Only `ipv6` field present
  - `dualstack` mode: Both fields present
- Empty string indicates STUN discovery failed for that protocol
- The `key` field is SHA-1 hash of peer identifier in hex format

#### Shell Plugin Protocol

The shell plugin provides a simpler alternative for shell scripts, using shell variable assignments instead of JSON.

**Input Format (stdin):**
```bash
STUNMESH_ACTION=get
STUNMESH_KEY=3061b8fcbdb6972059518f1adc3590dca6a5f352  # SHA-1 hash of peer identifier (hex)
STUNMESH_VALUE=a1b2c3d4e5f6...  # Hex-encoded encrypted endpoint data (for "set" only)
```

**Output:**
- For `get`: Write the hex-encoded value to stdout
- For `set`: Exit with code 0 for success
- For errors: Exit with non-zero code, error message on stderr

**Notes:**
- Both `STUNMESH_KEY` and `STUNMESH_VALUE` are hex strings (no special characters)
- The value format is identical to the exec plugin (hex-encoded encrypted JSON)
- No escaping or quoting needed - safe to use with `source /dev/stdin` or `eval`

**Example: Cloudflare DNS Storage (Shell Script)**
```bash
#!/bin/bash
source /dev/stdin

ZONE_ID="your-zone-id"
API_TOKEN="your-api-token"
SUBDOMAIN="wg"

case "$STUNMESH_ACTION" in
    get)
        # Get TXT record from Cloudflare
        # Record name format: <key>.<subdomain>.example.com
        RECORD_NAME="${STUNMESH_KEY}.${SUBDOMAIN}.example.com"
        VALUE=$(curl -s -X GET \
            "https://api.cloudflare.com/client/v4/zones/$ZONE_ID/dns_records?type=TXT&name=$RECORD_NAME" \
            -H "Authorization: Bearer $API_TOKEN" | \
            jq -r '.result[0].content' 2>/dev/null)

        if [ "$VALUE" != "null" ] && [ -n "$VALUE" ]; then
            echo "$VALUE"
        else
            echo "Record not found" >&2
            exit 1
        fi
        ;;
    set)
        # Create/update TXT record in Cloudflare
        RECORD_NAME="${STUNMESH_KEY}.${SUBDOMAIN}.example.com"
        curl -s -X POST \
            "https://api.cloudflare.com/client/v4/zones/$ZONE_ID/dns_records" \
            -H "Authorization: Bearer $API_TOKEN" \
            -H "Content-Type: application/json" \
            --data "{\"type\":\"TXT\",\"name\":\"$RECORD_NAME\",\"content\":\"$STUNMESH_VALUE\",\"ttl\":120}" \
            >/dev/null
        ;;
esac
```

**Configuration Example:**
```yaml
plugins:
  cf_shell:
    type: shell
    command: "/usr/local/bin/cloudflare-storage.sh"
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

### Ping Monitoring

stunmesh-go supports intelligent ping monitoring to detect tunnel health and automatically trigger reconnection when issues are detected.

#### Features

- **Per-peer ping monitoring**: Each peer can have its own target IP and ping settings
- **Adaptive retry logic**: Intelligent failure handling with exponential backoff
- **Automatic recovery**: Triggers publish/establish operations on ping failures
- **Configurable timeouts**: Per-peer or global timeout and interval settings

#### How It Works

1. **Normal Operation**: Pings target IP at configured intervals (constant frequency)
2. **Failure Detection**: When ping fails, immediately triggers publish and establish for the specific failing peer  
3. **Separate Retry Logic**: 
   - **Ping monitoring**: Continues at constant configured interval regardless of failures
   - **Publish/Establish retries**: Independent schedule with adaptive backoff
   - All retries: Always publish endpoint for the specific failing peer, then establish connection
   - First 3 retries: Fixed 2-second intervals  
   - After 3 retries: Arithmetic progression backoff (10s, 12s, 14s, 16s, 18s...)
   - Cap at `refresh_interval`: Hands over to normal refresh cycle, ping monitoring continues
4. **Recovery**: On successful ping, resets retry logic and re-enables publish/establish

#### Configuration

**Global Ping Settings:**
```yaml
ping_monitor:
  interval: "5s"        # Default ping interval
  timeout: "2s"         # Default ping timeout
  fixed_retries: 3      # Fixed retry attempts before exponential backoff
```

**Per-Peer Ping Configuration:**
```yaml
interfaces:
  wg0:
    peers:
      "peer1":
        public_key: "<PUBLIC_KEY_IN_BASE64>"
        plugin: cloudflare1
        ping:
          enabled: true
          target: "192.168.1.100"     # IP to ping through tunnel
          interval: "60s"             # Override global interval (optional)
          timeout: "10s"              # Override global timeout (optional)
      "peer2":
        public_key: "<PUBLIC_KEY_IN_BASE64>"
        plugin: cloudflare2
        ping:
          enabled: true
          target: "10.0.0.50"
          # Uses global interval and timeout defaults
      "peer3":
        public_key: "<PUBLIC_KEY_IN_BASE64>"
        plugin: cloudflare2
        # No ping configuration = ping monitoring disabled
```

#### Configuration Parameters

**Note: The entire `ping` section is optional. If omitted, ping monitoring is disabled for that peer.**

- **`enabled`**: Enable ping monitoring for this peer (default: false)
- **`target`**: IP address to ping through the tunnel (required if enabled)
- **`interval`**: How often to ping (optional, uses global default)
- **`timeout`**: Max time to wait for ping response (optional, uses global default)

#### Limitations

- **IPv4 Only**: Ping monitoring currently supports IPv4 addresses only
- **IP Address Required**: The `target` field must be an IP address, not a domain name
- **STUN Protocol Independence**: Ping monitoring protocol is independent of STUN protocol configuration
  - You can use IPv6 STUN discovery with IPv4 ping targets
  - Ping always uses IPv4 regardless of interface protocol setting
- **Examples**:
  - ✅ Valid: `"192.168.1.100"`, `"10.0.0.1"`, `"172.16.0.50"`
  - ❌ Invalid: `"router.local"`, `"google.com"`, `"2001:db8::1"` (IPv6 not supported for ping)

#### Use Cases

- **Tunnel Health Monitoring**: Detect when WireGuard tunnel stops working
- **Automatic Recovery**: Reconnect without manual intervention
- **Network Redundancy**: Faster failover than waiting for refresh_interval
- **Proactive Maintenance**: Identify and fix connectivity issues quickly

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
    type: exec
    command: "/usr/local/bin/stunmesh-cloudflare"
    args: ["-zone", "<ZONE_NAME>", "-token", "<API_TOKEN>"]
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
    type: exec
    command: "/usr/local/bin/stunmesh-cloudflare"
    args: ["-zone", "<ZONE_NAME>", "-token", "<API_TOKEN>"]
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
    type: exec
    command: "/usr/local/bin/stunmesh-cloudflare"
    args: ["-zone", "<ZONE_NAME>", "-token", "<API_TOKEN>"]
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
    type: exec
    command: "/usr/local/bin/stunmesh-cloudflare"
    args: ["-zone", "<ZONE_NAME>", "-token", "<API_TOKEN>"]
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
