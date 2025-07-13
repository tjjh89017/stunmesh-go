# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

stunmesh-go is a WireGuard helper tool that enables peer-to-peer connections through Full-Cone NAT using STUN discovery and encrypted peer information storage. It uses a flexible plugin system for storing peer endpoint data across multiple storage backends.

## Build and Development Commands

### Building
```bash
make build          # Build the main binary
make all           # Build everything (alias for build)  
make clean         # Clean build artifacts
go build -o stunmesh-go  # Direct go build
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
- Manages multiple named instances (e.g., `cloudflare1`, `cloudflare2`)
- Each peer can reference a different plugin instance

**Supported Plugin Types**:
- `cloudflare`: DNS TXT record storage via Cloudflare API
- `exec`: External process communication via JSON stdin/stdout

**Configuration Structure**:
```yaml
plugins:
  plugin_name:
    type: cloudflare|exec
    # Plugin-specific configuration
interfaces:
  wg0:
    peers:
      peer_name:
        public_key: "base64_encoded_key"
        plugin: plugin_name  # References named plugin instance
```

### Controller Architecture
Four main controllers orchestrate the application workflow:

1. **BootstrapController** (`internal/ctrl/bootstrap.go`):
   - Initializes WireGuard devices and discovers existing peers
   - Uses `FilterPeerService` to match config peers with device peers

2. **PublishController** (`internal/ctrl/publish.go`):
   - Performs STUN discovery to get public IP/port
   - Encrypts endpoint data and stores via configured plugins
   - Each peer uses its designated plugin instance

3. **EstablishController** (`internal/ctrl/establish.go`):
   - Retrieves peer endpoint data from storage plugins
   - Decrypts and configures WireGuard peer endpoints

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
- **Repositories** (`internal/repo/`): Data access abstractions
- **Configuration** (`internal/config/`): YAML config loading and device peer management

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

### Peer Entity Plugin Field
All `entity.NewPeer()` calls require a plugin parameter. When adding new peer creation code, ensure the plugin field is provided.

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
  "operation": "get|set",
  "key": "peer_identifier", 
  "value": "encrypted_data_for_set"
}
```

**Response Format**:
```json
{
  "success": true|false,
  "value": "encrypted_data_for_get",
  "error": "error_message_if_failed"
}
```

## Testing Patterns

### Mock Usage
Tests extensively use mocks for external dependencies:
- `MockWireGuardClient` for WireGuard operations
- `MockConfigPeerProvider` and `MockDevicePeerChecker` for filter service tests
- `MockPeerRepository` and `MockDeviceRepository` for storage

### Constructor Parameter Updates
When `entity.NewPeer()` signature changes, update all test files with the new parameter requirements (typically adding plugin parameter).

## Common Development Scenarios

### Adding New Plugin Types
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