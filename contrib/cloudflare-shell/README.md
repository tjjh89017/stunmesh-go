# Cloudflare DNS Storage Plugin (Shell Script Version)

A shell script implementation of the Cloudflare DNS TXT record storage plugin, demonstrating the **shell plugin protocol**.

This is an alternative to the Go-based [cloudflare plugin](../cloudflare/) that uses pure shell scripting with curl. It's simpler but requires `curl` and `grep/sed` to be available.

## Features

- Uses Cloudflare DNS TXT records to store peer endpoint data
- Pure shell script - no compilation needed
- Uses the simplified shell plugin protocol (no JSON parsing required)
- Automatically handles record creation and updates
- No external dependencies beyond `curl`, `grep`, and `sed`

## Requirements

- `curl`
- `grep`
- `sed`
- Cloudflare account with API token

## Installation

### From Source

1. Build and install the script:
   ```bash
   cd contrib/cloudflare-shell
   make install
   # Or specify custom installation path:
   # make install PREFIX=/opt/local
   ```

### From Release

1. Download from GitHub releases and install:
   ```bash
   # Extract from plugin archive
   unzip stunmesh-plugins-linux-amd64-v1.4.0.zip
   sudo mv stunmesh-cloudflare-shell /usr/local/bin/
   sudo chmod +x /usr/local/bin/stunmesh-cloudflare-shell
   ```

2. Get your Cloudflare API token:
   - Go to https://dash.cloudflare.com/profile/api-tokens
   - Create a token with "Zone.DNS" permissions

## Configuration

Add to your `config.yaml`:

```yaml
plugins:
  cf_shell:
    type: shell
    command: /usr/local/bin/stunmesh-cloudflare-shell
    args:
      - "-zone"
      - "example.com"          # Your Cloudflare zone (domain)
      - "-token"
      - "your-api-token"       # Your Cloudflare API token
      - "-subdomain"
      - "wg"                   # Optional: subdomain prefix

interfaces:
  wg0:
    peers:
      peer1:
        public_key: "..."
        plugin: cf_shell
```

## How It Works

The script uses the shell plugin protocol:

1. **Input (stdin)**: Shell variables
   ```bash
   STUNMESH_ACTION=get
   STUNMESH_KEY=3061b8fcbdb6972059518f1adc3590dca6a5f352
   STUNMESH_VALUE=abc123...  # for set only
   ```

2. **Output**:
   - For `get`: Prints value to stdout
   - For `set`: Exits with code 0 on success
   - For errors: Exits non-zero with error message on stderr

3. **DNS Record Format**:
   - Without subdomain: `<key>.example.com`
   - With subdomain: `<key>.wg.example.com`

## Comparison with Go Plugin

| Feature | Shell Version | Go Version |
|---------|--------------|------------|
| Installation | Copy script | Compile binary |
| Dependencies | curl, grep, sed | None (static binary) |
| Protocol | Shell variables | JSON |
| Performance | Slightly slower | Faster |
| Ease of modification | Very easy | Requires Go knowledge |

## Troubleshooting

**Error: "Failed to get zone ID"**
- Check that your zone name is correct
- Verify your API token has Zone.DNS permissions

**Error: "Record not found"**
- The peer hasn't published its endpoint yet
- DNS propagation may take a few seconds

**No output on get operation**
- Check Cloudflare dashboard for the TXT record
- Verify the record name format matches

## Example: Manual Testing

Test the script manually:

```bash
# Test GET operation
echo -e "STUNMESH_ACTION=get\nSTUNMESH_KEY=testkey123" | \
  /usr/local/bin/stunmesh-cloudflare-shell \
  -zone example.com \
  -token YOUR_TOKEN \
  -subdomain wg

# Test SET operation
echo -e "STUNMESH_ACTION=set\nSTUNMESH_KEY=testkey123\nSTUNMESH_VALUE=testvalue456" | \
  /usr/local/bin/stunmesh-cloudflare-shell \
  -zone example.com \
  -token YOUR_TOKEN \
  -subdomain wg
```

## Security Notes

- Store your API token securely (use environment variables or secure config management)
- Consider using a scoped API token with minimal permissions (Zone.DNS only)
- The script does not validate the zone_id - ensure you're using the correct zone

## See Also

- [Shell Plugin Protocol](../../README.md#shell-plugin-protocol)
- [Go-based Cloudflare Plugin](../cloudflare/)
- [Contrib Plugin Development Guide](../README.md)
