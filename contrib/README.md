# Stunmesh Contrib Plugins

This directory contains standalone plugins for stunmesh that extend its functionality.

## Available Plugins

### Cloudflare DNS Plugin

Location: [`cloudflare/`](cloudflare/)

Stores peer endpoint information in Cloudflare DNS TXT records. Useful for scenarios where you want to use your existing Cloudflare DNS infrastructure for peer discovery.

**Features:**
- DNS TXT record storage
- Optional subdomain support
- Automatic record management

See [cloudflare/README.md](cloudflare/README.md) for setup instructions.

## Creating Your Own Plugin

Stunmesh plugins communicate via JSON over stdin/stdout. To create a new plugin:

1. Create a new directory under `contrib/`
2. **Recommended**: Write your plugin in Go with `CGO_ENABLED=0`
   - Go with `CGO_ENABLED=0` produces fully static binaries
   - Works perfectly with the `FROM scratch` Docker image (minimal size, no dependencies)
   - No need for libc or other runtime libraries in the container
   - Easier to maintain consistency with the main project
   - See [cloudflare/](cloudflare/) as a reference implementation
3. Name your binary with the `stunmesh-*` prefix (e.g., `stunmesh-yourplugin`)
   - **Required**: This naming convention allows the Dockerfile to automatically include your plugin
   - The Dockerfile uses pattern `/work/contrib/*/stunmesh-*` to copy all plugins to `/app/`
   - The `/app/` directory is added to `PATH` in the Docker container
4. Implement the exec plugin protocol:
   - Read JSON requests from stdin
   - Write JSON responses to stdout
5. Support two operations: `get` and `set`

### Plugin Protocol

**Request Format:**
```json
{
  "operation": "get|set",
  "key": "peer_identifier",
  "value": "data_for_set_operation"
}
```

**Response Format:**
```json
{
  "success": true|false,
  "value": "data_for_get_operation",
  "error": "error_message_if_failed"
}
```

See the [exec plugin documentation](../README.md) for more details.

## Contributing

We welcome contributions of new plugins! Please ensure your plugin:

- Follows the exec plugin protocol
- **Recommended**: Written in Go with `CGO_ENABLED=0` for minimal Docker image size
- Uses the `stunmesh-*` naming convention for the binary
- Includes a Makefile with `build`, `clean`, `install`, and `uninstall` targets
  - Makefile should default to `CGO_ENABLED=0`
- Includes a README with setup instructions
- Is well-tested and handles errors gracefully
- Includes example configuration

Submit pull requests to the main stunmesh-go repository.
