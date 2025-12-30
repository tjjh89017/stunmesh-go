# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

stunmesh-go is a WireGuard helper tool that enables peer-to-peer connections through Full-Cone NAT using STUN discovery and encrypted peer information storage. It uses a flexible plugin system for storing peer endpoint data across multiple storage backends.

## Build and Development Commands

### Building
```bash
make build          # Build the main binary (includes all built-in plugins by default)
make all           # Build everything (clean + build)
make clean         # Clean build artifacts
make plugin        # Build all contrib plugins
make contrib       # Alias for 'make plugin'
go build -o stunmesh-go  # Direct go build

# Built-in plugin options
make build BUILTIN=all                    # Build with all built-in plugins (default)
make build BUILTIN=                       # Build without any built-in plugins (minimal)
make build BUILTIN=builtin_cloudflare     # Build with specific built-in plugin only
```

### Testing
```bash
go test ./...                    # Run all tests
go test ./internal/ctrl -v       # Run controller tests with verbose output
go test ./internal/entity -v     # Run entity tests
go test ./internal/repo -v       # Run repository tests
```

### Dependency Management
```bash
go mod tidy                      # Clean up dependencies
go generate ./internal/entity    # Regenerate mocks (requires mockgen)
go generate wire.go              # Regenerate wire dependency injection
```

### Installing Dependencies for Development
```bash
go install go.uber.org/mock/mockgen@latest  # For generating mocks
go mod download go.uber.org/mock            # Download mock framework
```

## Core Architecture

### Dependency Injection with Google Wire
The application uses Google Wire for dependency injection. The main setup is in `wire.go`:
- `wire.go`: Defines dependency graph (build tag: `wireinject`)
- `wire_gen.go`: Generated dependency injection code (do not edit manually)
- Run `go generate wire.go` after changing dependencies

### Plugin System Architecture
The plugin system supports multiple named storage backend instances:

**Plugin Manager** (`plugin/manager.go`):
- Factory pattern for creating plugin instances by type
- Manages multiple named instances (e.g., `exec1`, `exec2`)
- Each peer can reference a different plugin instance

**Supported Plugin Types**:
- `builtin`: Compiled into the binary (default: includes all built-in plugins like cloudflare)
- `exec`: External process communication via JSON stdin/stdout (for complex plugins)
- `shell`: Simplified protocol using shell variables via stdin (for simple shell scripts)

**Contrib Plugins** (`contrib/` directory):
- Cloudflare DNS plugin: Standalone exec plugin in `contrib/cloudflare/`
- Additional community plugins can be added to `contrib/`
- Plugin Makefiles should use `PLUGIN` variable (not `APP`) to avoid conflicts with workflow-level environment variables
- Plugins are built with `make plugin` or `make contrib` from the root directory

**Configuration Structure**:
```yaml
plugins:
  # Built-in plugin (compiled into binary, available by default)
  cloudflare_builtin:
    type: builtin
    name: cloudflare
    zone_name: example.com
    api_token: ${CLOUDFLARE_API_TOKEN}
    subdomain: stunmesh

  # External exec plugin
  cloudflare_exec:
    type: exec
    command: /usr/local/bin/stunmesh-cloudflare
    args: ["-zone", "example.com", "-token", "${CLOUDFLARE_API_TOKEN}"]

interfaces:
  wg0:
    protocol: "ipv4"  # Interface protocol: ipv4, ipv6, or dualstack
    peers:
      peer_name:
        public_key: "base64_encoded_key"
        plugin: cloudflare_builtin  # References named plugin instance
        protocol: "ipv4"             # Peer protocol: ipv4, ipv6, prefer_ipv4, prefer_ipv6
```

### Controller Architecture
Four main controllers orchestrate the application workflow:

1. **BootstrapController** (`internal/ctrl/bootstrap.go`):
   - Initializes WireGuard devices and discovers existing peers
   - Uses `FilterPeerService` to match config peers with device peers
   - Reads device protocol from config and stores in Device entity

2. **PublishController** (`internal/ctrl/publish.go`):
   - Performs STUN discovery to get public IP/port based on device protocol
   - Supports three modes: `ipv4` (IPv4 only), `ipv6` (IPv6 only), `dualstack` (both)
   - Uses `discoverEndpoints()` helper method for protocol-aware STUN discovery
   - **Validation**: `Resolver.Resolve()` validates endpoints (port != 0 and host != "")
     - Invalid endpoints are not published to storage
     - Logs warning when STUN returns invalid endpoint
   - Encrypts entire endpoint JSON (`{"ipv4": "...", "ipv6": "..."}`) and stores via plugins
   - Each peer uses its designated plugin instance
   - Logs discovered endpoints for debugging

3. **EstablishController** (`internal/ctrl/establish.go`):
   - Retrieves peer endpoint data from storage plugins
   - Decrypts endpoint JSON and selects appropriate endpoint based on peer protocol:
     - `ipv4`: Use IPv4 endpoint only (returns error if empty)
     - `ipv6`: Use IPv6 endpoint only (returns error if empty)
     - `prefer_ipv4`: Prefer IPv4, fallback to IPv6 if unavailable
     - `prefer_ipv6`: Prefer IPv6, fallback to IPv4 if unavailable
   - **No port validation needed**: Since publish validates before storage, port should never be 0
   - Configures WireGuard peer with selected endpoint

4. **RefreshController** (`internal/ctrl/refresh.go`):
   - Queues peer refresh operations on a periodic schedule

### FilterPeerService Pattern
Key architectural pattern in `internal/entity/filter_peer.go`:
```go
// Gets peers from config (with plugin info) first
configPeers := configProvider.GetConfigPeers(ctx, deviceName, publicKey)

// Efficiently checks which exist in WireGuard device  
devicePeerMap := deviceChecker.GetDevicePeerMap(ctx, deviceName)

// Returns only config peers that exist in device (preserving plugin info)
```

This reversed approach ensures peers have complete plugin configuration throughout the data flow.

### Entity and Repository Layers
- **Entities** (`internal/entity/`): Domain objects (`Peer`, `Device`, `PeerId`)
  - `Device`: Contains `protocol` field (interface-level protocol configuration)
  - `Peer`: Contains `protocol` field (peer-level protocol preference)
- **Repositories** (`internal/repo/`): Data access abstractions
- **Configuration** (`internal/config/`): YAML config loading and device peer management

### Protocol Configuration Architecture

The protocol configuration operates at two distinct levels:

**Interface Protocol** (Device Level):
- Controls which STUN discovery protocols are performed
- Values: `ipv4`, `ipv6`, `dualstack`
- Default: `ipv4` (for backward compatibility)
- Validation in `config.DeviceConfig.GetInterfaceProtocol()`
- Stored in `entity.Device.protocol` field

**Peer Protocol** (Peer Level):
- Controls which endpoint to use when establishing connections
- Values: `ipv4`, `ipv6`, `prefer_ipv4`, `prefer_ipv6`
- Default: `ipv4` (for backward compatibility)
- Validation in `config.Peer.GetProtocol()`
- Stored in `entity.Peer.protocol` field

**Data Flow**:
1. `BootstrapController` reads protocol from config → creates Device/Peer entities with protocol
2. `PublishController` uses `device.Protocol()` → performs appropriate STUN discovery → stores endpoints
3. `EstablishController` uses `peer.Protocol()` → selects appropriate endpoint → configures WireGuard

### STUN Implementation

**Platform-Specific Implementations**:
- **Linux** (`internal/stun/stun_linux.go`): Raw IP sockets with BPF filters
- **Darwin/BSD** (`internal/stun/stun_darwinbsd.go`): pcap with BPF filters

**IPv6 Support**:
- Uses `golang.org/x/net/ipv6.PacketConn` for IPv6 raw sockets
- IPv6 UDP checksum is **mandatory** (RFC 8200) - handled by kernel via `SetChecksum(true, 6)`
- Network strings: `ip6:17` for raw socket, `udp6` for STUN server connection
- For IPv6, raw socket addresses must use `net.IPAddr` (not `net.UDPAddr`) when sending packets

**Linux-Specific Behavior**:
- **IP Header Stripping**: Linux kernel strips IP headers for both IPv4 and IPv6 raw sockets at application layer
- **BPF Filter Timing**: BPF filters run at different stages for IPv4 vs IPv6:
  - **IPv4**: BPF filter sees full packet (IP header + UDP header + payload)
    - BPF offsets: dst_port at 22, STUN magic at 32
  - **IPv6**: BPF filter sees packet without IP header (UDP header + payload)
    - BPF offsets: dst_port at 2, STUN magic at 12
- **Application Layer**: Both protocols receive UDP header (8 bytes) + payload
  - Application offset: Always 8 bytes (skip UDP header)

**Darwin/BSD-Specific Behavior**:
- Uses pcap with BPF filters
- Different link layer types (Null/Loopback vs Ethernet) require different BPF offsets
- IPv6 BPF filter checks EtherType (0x86DD) for Ethernet frames

**Key Methods**:
- `stun.New(ctx, deviceName, port, protocol)`: Creates STUN client with specified protocol
- `resolver.Resolve(ctx, deviceName, port, protocol)`: Performs STUN discovery and returns endpoint
  - Returns error if port == 0 or host == "" (validates endpoint before returning)
- `stun.Connect(ctx, stunAddr)`: Sends STUN request and receives response
  - Converts `net.UDPAddr` to `net.IPAddr` for raw socket transmission

### Encryption and Storage Format

**Endpoint Data Format**:
```json
{
  "ipv4": "1.2.3.4:5678",
  "ipv6": "[2001:db8::1]:5678"
}
```

**Encryption**:
- Entire JSON is encrypted using NaCl box (Curve25519 + XSalsa20 + Poly1305)
- Encrypted data is hex-encoded for storage
- Plugin stores/retrieves hex string (no JSON parsing needed in plugin)

**Decryption and Selection**:
- Controller decrypts entire JSON
- Selects endpoint based on peer protocol preference
- Supports fallback logic for `prefer_ipv4`/`prefer_ipv6`

## Configuration System

### Config Loading Priority
Configuration is loaded from these paths in order:
1. `$STUNMESH_CONFIG_DIR/config.yaml`
2. `/etc/stunmesh/config.yaml` 
3. `$HOME/.stunmesh/config.yaml`
4. `./config.yaml`

### Environment Variable Support
Uses Viper's `AutomaticEnv()` - environment variables can override config values using `STUNMESH_` prefix.

## Key Implementation Details

### Wire Interface Bindings
Critical interface bindings in `wire.go`:
```go
wire.Bind(new(entity.ConfigPeerProvider), new(*config.DeviceConfig))
wire.Bind(new(entity.DevicePeerChecker), new(*repo.Peers))
```

### Entity Constructor Signatures

**Device Entity**:
```go
entity.NewDevice(name DeviceId, listenPort int, privateKey []byte, protocol string) *Device
```
- `protocol`: Must be "ipv4", "ipv6", or "dualstack"

**Peer Entity**:
```go
entity.NewPeer(id PeerId, deviceName string, publicKey [32]byte, plugin string, protocol string, pingConfig PeerPingConfig) *Peer
```
- `plugin`: Required - references the plugin instance name from config
- `protocol`: Must be "ipv4", "ipv6", "prefer_ipv4", or "prefer_ipv6"

When adding new entity creation code, ensure all required parameters are provided with correct values.

### Mock Generation
Mocks are generated using `go.uber.org/mock/mockgen`. Interface changes require regenerating mocks:
```bash
go generate ./internal/entity  # Regenerates peer-related mocks
```

### Exec Plugin Protocol
External storage scripts communicate via JSON stdin/stdout:

**Request Format**:
```json
{
  "action": "get|set",
  "key": "peer_identifier_sha1_hex",
  "value": "encrypted_data_hex"
}
```

**Response Format**:
```json
{
  "success": true|false,
  "value": "encrypted_data_hex",
  "error": "error_message_if_failed"
}
```

### Shell Plugin Protocol
Simplified protocol for shell scripts using shell variable assignments:

**Input (stdin)**:
```bash
STUNMESH_ACTION=get
STUNMESH_KEY=3061b8fcbdb6972059518f1adc3590dca6a5f352  # SHA1 hex
STUNMESH_VALUE=a1b2c3d4e5f6...  # hex (for set only)
```

**Output**:
- For `get`: stdout contains the value (hex string)
- For `set`: exit code 0 for success
- For errors: exit non-zero, stderr contains error message

**Notes**:
- Both key and value are hex-encoded strings (no special characters)
- No escaping or quoting needed - can safely use `source /dev/stdin` or `eval`
- Simpler than JSON protocol for basic shell scripts

## Testing Patterns

### Mock Usage
Tests extensively use mocks for external dependencies:
- `MockWireGuardClient` for WireGuard operations
- `MockConfigPeerProvider` and `MockDevicePeerChecker` for filter service tests
- `MockPeerRepository` and `MockDeviceRepository` for storage

### Constructor Parameter Updates
When `entity.NewPeer()` or `entity.NewDevice()` signatures change, update all affected files:
- Test files in `internal/repo/*_test.go`
- Test files in `internal/ctrl/*_test.go`
- Test files in `internal/crypto/*_test.go`
- Controller implementations in `internal/ctrl/bootstrap.go`

Common parameters to remember:
- Both Device and Peer require `protocol` parameter
- Peer requires `plugin` parameter (references config plugin name)
- Use "ipv4" as default protocol value in tests for backward compatibility

## Common Development Scenarios

### Adding New Contrib Plugins
1. Create a new directory under `contrib/` (e.g., `contrib/myplugin/`)
2. Create a `Makefile` with:
   - Use `PLUGIN ?= stunmesh-<name>` variable (not `APP`)
   - Implement `build`, `clean`, `install`, `uninstall` targets
   - Set `CGO_ENABLED ?= 0` for static binaries (recommended)
3. Implement the exec plugin protocol (JSON stdin/stdout)
4. GitHub Actions will automatically build and release the plugin for all supported platforms
5. See `contrib/README.md` for detailed plugin development guide

### Adding New Plugin Types to Core
1. Add new `PluginType` constant in `plugin/manager.go`
2. Implement `Store` interface in new plugin file
3. Add case in `createPlugin()` method
4. Update documentation in README.md

### Modifying Wire Dependencies
1. Update `wire.go` with new bindings
2. Run `go generate wire.go` to regenerate `wire_gen.go`
3. Build to verify dependency resolution

### Interface Changes
1. Update interface definition
2. Update all implementations
3. Regenerate mocks: `go generate ./internal/entity`
4. Update tests to match new signatures

## GitHub Actions CI/CD

### Workflow Structure
- **main.yml**: Build and test on PR/push to main
  - `build` job: Builds main binary for all OS/arch combinations
  - `build-plugins` job: Builds all contrib plugins (runs in parallel with `build`)
  - Both depend on `lint` and `test` jobs

- **release.yml**: Release binaries on tag push
  - `release` job: Releases main binary
  - `release-plugins` job: Releases plugin archives (runs in parallel with `release`)
  - Plugin binaries are packaged per OS/arch (e.g., `stunmesh-plugins-linux-amd64.zip`)

### Plugin Build System
- Uses `.github/actions/build-all-plugins/` to build all plugins
- Automatically discovers plugins in `contrib/` directory
- Sets `unset APP` to avoid conflicts with workflow-level `env.APP`
- Creates `plugins_dist/` directory with all built binaries
- Supports Linux, Darwin (macOS), and FreeBSD with multiple architectures