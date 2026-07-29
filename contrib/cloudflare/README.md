# Stunmesh Cloudflare Plugin

This is a standalone exec plugin for stunmesh that stores peer endpoint information in Cloudflare DNS TXT records.

## Features

- Stores encrypted peer endpoint data in DNS TXT records
- Supports optional subdomains for DNS record organization
- Automatic record creation, update, and duplicate cleanup
- Full integration with stunmesh exec plugin system

## Building

```bash
cd contrib/cloudflare
go build -o stunmesh-cloudflare
```

## Usage

The plugin accepts configuration via command-line flags:

- `-zone` (required): Your Cloudflare DNS zone name (e.g., `example.com`)
- `-token` (required): Your Cloudflare API token
- `-subdomain` (optional): Subdomain prefix for DNS records

### Creating a Cloudflare API Token

1. Go to Cloudflare Dashboard > My Profile > API Tokens
2. Click "Create Token"
3. Use the "Edit zone DNS" template
4. Select your zone under "Zone Resources"
5. Create and copy the token

## Configuration with Stunmesh

Configure the plugin in your stunmesh `config.yaml`:

### Example 1: Basic configuration

```yaml
plugins:
  cf1:
    type: exec
    command: /path/to/stunmesh-cloudflare
    args:
      - "-zone"
      - "example.com"
      - "-token"
      - "your_cloudflare_api_token"

interfaces:
  wg0:
    peers:
      peer1:
        public_key: "base64_encoded_key"
        plugin: cf1
```

### Example 1b: Docker configuration

When using the official Docker image, plugins are in PATH:

```yaml
plugins:
  cf1:
    type: exec
    command: stunmesh-cloudflare  # No path needed in Docker
    args:
      - "-zone"
      - "example.com"
      - "-token"
      - "your_cloudflare_api_token"

interfaces:
  wg0:
    peers:
      peer1:
        public_key: "base64_encoded_key"
        plugin: cf1
```

### Example 2: With subdomain

```yaml
plugins:
  cf1:
    type: exec
    command: /path/to/stunmesh-cloudflare
    args:
      - "-zone"
      - "example.com"
      - "-token"
      - "your_cloudflare_api_token"
      - "-subdomain"
      - "stunmesh"

interfaces:
  wg0:
    peers:
      peer1:
        public_key: "base64_encoded_key"
        plugin: cf1
```

## How It Works

The plugin communicates with stunmesh via JSON over stdin/stdout using the exec plugin protocol.

### Get Operation

When stunmesh needs to retrieve peer endpoint data:

**Request (stdin)**:
```json
{
  "action": "get",
  "key": "peer_identifier"
}
```

**Response (stdout)**:
```json
{
  "success": true,
  "value": "encrypted_endpoint_data"
}
```

### Set Operation

When stunmesh needs to store peer endpoint data:

**Request (stdin)**:
```json
{
  "action": "set",
  "key": "peer_identifier",
  "value": "encrypted_endpoint_data"
}
```

**Response (stdout)**:
```json
{
  "success": true
}
```

### DNS Record Format

DNS records are created in the format:
- With subdomain: `<key>.subdomain.zone_name`
- Without subdomain: `<key>.zone_name`

`key` is the SHA1 hex digest stunmesh already derives per peer before calling
the plugin, so it is used directly as the DNS-safe record name — matching
the builtin Cloudflare plugin and `cloudflare-shell.sh`.

### Compatibility note: record-name fix

Versions of this plugin prior to this fix hashed `key` a second time before
building the record name, which silently diverged from the builtin
Cloudflare plugin and from its own sibling, `cloudflare-shell.sh` — the two
were never interoperable, and no deployment could have relied on that
double-hashed name matching either of them. If you have already published
records under the old double-hashed name using this specific plugin, you
need to either:

- do nothing and let it self-heal: the next publish cycle's `Set` call will
  start writing to the corrected (single-hash) record name automatically, or
- pin to the old binary until you are ready for that re-publish to happen.

The stale double-hashed record is not deleted automatically; it is simply
no longer read or written once you upgrade, so you may want to remove it
manually from Cloudflare after peers have re-published under the corrected
name.

## License

Same as stunmesh-go main project.
