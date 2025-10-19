# Stunmesh Contrib Plugins

This directory contains standalone plugins for stunmesh that extend its functionality.

## Available Plugins

### Cloudflare DNS Plugin (Go)

Location: [`cloudflare/`](cloudflare/)

Stores peer endpoint information in Cloudflare DNS TXT records. Useful for scenarios where you want to use your existing Cloudflare DNS infrastructure for peer discovery.

**Features:**
- DNS TXT record storage
- Optional subdomain support
- Automatic record management
- Compiled Go binary (static, no dependencies)

**Protocol:** Exec (JSON)

See [cloudflare/README.md](cloudflare/README.md) for setup instructions.

### Cloudflare DNS Plugin (Shell Script)

Location: [`cloudflare-shell/`](cloudflare-shell/)

Shell script implementation of the Cloudflare DNS plugin, demonstrating the **shell plugin protocol**. Uses pure shell scripting with curl instead of compiled Go code.

**Features:**
- Same functionality as Go version
- No compilation needed
- Uses simplified shell variable protocol
- Requires curl, grep, sed

**Protocol:** Shell (shell variables)

**Best for:** Learning, quick testing, or environments where shell scripts are preferred over compiled binaries.

See [cloudflare-shell/README.md](cloudflare-shell/README.md) for setup instructions.

## Creating Your Own Plugin

Stunmesh supports two plugin protocols: **exec** (JSON-based) and **shell** (shell variable-based).

### Choose Your Protocol

**Exec Protocol (JSON)** - Best for:
- Complex plugins with error handling
- Plugins written in Go, Python, Node.js, etc.
- When you need structured data

**Shell Protocol (shell variables)** - Best for:
- Simple shell scripts
- Quick prototypes or learning
- When you want to avoid JSON parsing

### General Steps

1. Create a new directory under `contrib/`
2. Choose your implementation approach:

   **Option A: Go Plugin (Recommended for production)**
   - Write in Go with `CGO_ENABLED=0` for static binaries
   - Works with `FROM scratch` Docker images (minimal size)
   - See [cloudflare/](cloudflare/) as reference
   - Use exec protocol (JSON)

   **Option B: Shell Script Plugin**
   - Write a shell script using bash/sh
   - No compilation needed
   - See [cloudflare-shell/](cloudflare-shell/) as reference
   - Use shell protocol (variables)

3. Name your plugin with `stunmesh-*` prefix (e.g., `stunmesh-yourplugin`)
   - **Required**: Dockerfile automatically includes plugins matching this pattern
   - The `/app/` directory is added to `PATH` in Docker containers

4. Create a Makefile with standard targets:
   - Use `PLUGIN` variable (not `APP`) to avoid conflicts
   - Implement: `build`, `clean`, `install`, `uninstall`
   - For Go plugins: set `CGO_ENABLED ?= 0`
   - For shell scripts: `build` target should set executable permissions

5. Support two operations: `get` and `set`

### Exec Plugin Protocol (JSON)

Use this protocol for complex plugins.

**Request Format (stdin):**
```json
{
  "action": "get|set",
  "key": "peer_identifier_sha1_hex",
  "value": "encrypted_data_hex"
}
```

**Response Format (stdout):**
```json
{
  "success": true|false,
  "value": "encrypted_data_hex",
  "error": "error_message_if_failed"
}
```

See the [exec plugin documentation](../README.md#exec-plugin-protocol) for more details.

### Shell Plugin Protocol (Shell Variables)

Use this protocol for simple shell scripts.

**Input Format (stdin):**
```bash
STUNMESH_ACTION=get
STUNMESH_KEY=3061b8fcbdb6972059518f1adc3590dca6a5f352
STUNMESH_VALUE=abc123...  # Only for set operation
```

**Output:**
- For `get`: Write value to stdout, exit 0
- For `set`: Exit 0 on success
- For errors: Exit non-zero, write error to stderr

**Example Shell Script:**
```bash
#!/bin/bash
source /dev/stdin

case "$STUNMESH_ACTION" in
    get)
        # Retrieve and output value
        cat "/data/$STUNMESH_KEY"
        ;;
    set)
        # Store value
        echo "$STUNMESH_VALUE" > "/data/$STUNMESH_KEY"
        ;;
esac
```

**Important Notes:**
- Both `STUNMESH_KEY` and `STUNMESH_VALUE` are hex strings (SHA1 and encrypted data)
- No special characters - safe to use without quoting or escaping
- Can safely use `source /dev/stdin` or `eval`

See the [shell plugin documentation](../README.md#shell-plugin-protocol) and [cloudflare-shell/](cloudflare-shell/) for complete examples.

## Contributing

We welcome contributions of new plugins! Please ensure your plugin:

- Follows either the exec or shell plugin protocol
- **Recommended for production**: Written in Go with `CGO_ENABLED=0` for minimal Docker image size
- **Alternative**: Shell script for simple use cases (see cloudflare-shell example)
- Uses the `stunmesh-*` naming convention (required for Docker auto-inclusion)
- Includes a Makefile with `build`, `clean`, `install`, and `uninstall` targets
  - Makefile must use `PLUGIN` variable (not `APP`)
  - For Go plugins: default to `CGO_ENABLED=0`
  - For shell scripts: `build` target sets executable permissions
- Includes a README with:
  - Setup instructions
  - Configuration examples
  - Protocol type (exec or shell)
  - Dependencies (if any)
- Is well-tested and handles errors gracefully
- Handles both `get` and `set` operations correctly

Submit pull requests to the main stunmesh-go repository.
