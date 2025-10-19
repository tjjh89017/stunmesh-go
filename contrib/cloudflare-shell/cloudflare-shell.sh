#!/bin/bash
# Cloudflare DNS TXT Record Storage Plugin (Shell Version)
#
# This is a shell script implementation of the Cloudflare plugin,
# demonstrating the shell plugin protocol.
#
# Configuration in config.yaml:
#   plugins:
#     cf_shell:
#       type: shell
#       command: /path/to/stunmesh-cloudflare-shell
#       args: ["-zone", "example.com", "-token", "API_TOKEN", "-subdomain", "wg"]

set -e

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -zone)
            ZONE_NAME="$2"
            shift 2
            ;;
        -token)
            API_TOKEN="$2"
            shift 2
            ;;
        -subdomain)
            SUBDOMAIN="$2"
            shift 2
            ;;
        *)
            echo "Unknown option: $1" >&2
            exit 1
            ;;
    esac
done

# Validate required parameters
if [ -z "$ZONE_NAME" ] || [ -z "$API_TOKEN" ]; then
    echo "Missing required parameters: -zone, -token" >&2
    exit 1
fi

# Read shell variables from stdin
source /dev/stdin

# Get zone ID from zone name
ZONE_RESPONSE=$(curl -s -X GET \
    "https://api.cloudflare.com/client/v4/zones?name=${ZONE_NAME}" \
    -H "Authorization: Bearer ${API_TOKEN}" \
    -H "Content-Type: application/json")

ZONE_ID=$(echo "$ZONE_RESPONSE" | grep -o '"id":"[^"]*"' | head -1 | sed 's/"id":"\(.*\)"/\1/')

if [ -z "$ZONE_ID" ] || [ "$ZONE_ID" = "null" ]; then
    echo "Failed to get zone ID for zone: $ZONE_NAME" >&2
    exit 1
fi

# Construct record name: <key>.<subdomain>.<zone_name>
if [ -n "$SUBDOMAIN" ]; then
    RECORD_NAME="${STUNMESH_KEY}.${SUBDOMAIN}.${ZONE_NAME}"
else
    RECORD_NAME="${STUNMESH_KEY}.${ZONE_NAME}"
fi

case "$STUNMESH_ACTION" in
    get)
        # Get TXT record from Cloudflare
        RESPONSE=$(curl -s -X GET \
            "https://api.cloudflare.com/client/v4/zones/${ZONE_ID}/dns_records?type=TXT&name=${RECORD_NAME}" \
            -H "Authorization: Bearer ${API_TOKEN}" \
            -H "Content-Type: application/json")

        # Extract value using basic text processing (no jq dependency)
        VALUE=$(echo "$RESPONSE" | grep -o '"content":"[^"]*"' | head -1 | sed 's/"content":"\(.*\)"/\1/')

        if [ -n "$VALUE" ] && [ "$VALUE" != "null" ]; then
            echo "$VALUE"
        else
            echo "Record not found: $RECORD_NAME" >&2
            exit 1
        fi
        ;;

    set)
        # First, try to find existing record ID
        RESPONSE=$(curl -s -X GET \
            "https://api.cloudflare.com/client/v4/zones/${ZONE_ID}/dns_records?type=TXT&name=${RECORD_NAME}" \
            -H "Authorization: Bearer ${API_TOKEN}" \
            -H "Content-Type: application/json")

        RECORD_ID=$(echo "$RESPONSE" | grep -o '"id":"[^"]*"' | head -1 | sed 's/"id":"\(.*\)"/\1/')

        if [ -n "$RECORD_ID" ] && [ "$RECORD_ID" != "null" ]; then
            # Update existing record
            curl -s -X PUT \
                "https://api.cloudflare.com/client/v4/zones/${ZONE_ID}/dns_records/${RECORD_ID}" \
                -H "Authorization: Bearer ${API_TOKEN}" \
                -H "Content-Type: application/json" \
                --data "{\"type\":\"TXT\",\"name\":\"${RECORD_NAME}\",\"content\":\"${STUNMESH_VALUE}\",\"ttl\":120}" \
                >/dev/null
        else
            # Create new record
            curl -s -X POST \
                "https://api.cloudflare.com/client/v4/zones/${ZONE_ID}/dns_records" \
                -H "Authorization: Bearer ${API_TOKEN}" \
                -H "Content-Type: application/json" \
                --data "{\"type\":\"TXT\",\"name\":\"${RECORD_NAME}\",\"content\":\"${STUNMESH_VALUE}\",\"ttl\":120}" \
                >/dev/null
        fi

        echo "Successfully stored record: $RECORD_NAME" >&2
        ;;

    *)
        echo "Unknown action: $STUNMESH_ACTION" >&2
        exit 1
        ;;
esac
