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
- With subdomain: `<sha1_hash>.subdomain.zone_name`
- Without subdomain: `<sha1_hash>.zone_name`

The key is hashed using SHA-1 to create DNS-safe record names.

## License

Same as stunmesh-go main project.
