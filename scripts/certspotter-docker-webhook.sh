#!/bin/sh
# Webhook script for certspotter running in Docker
# Transforms certspotter JSON to Canary format and forwards

# Certspotter passes JSON_FILENAME env var pointing to the JSON file
if [ -z "$JSON_FILENAME" ]; then
    echo "ERROR: JSON_FILENAME not set" >&2
    exit 1
fi

# Read the JSON file
CERT_JSON=$(cat "$JSON_FILENAME")

# Transform certspotter format to Canary format using jq
# Certspotter: {"dns_names":[],"tbs_sha256":"..."}
# Canary: {"issuance":{"dns_names":[],"tbs_sha256":"..."},"id":"cert_id"}
TRANSFORMED_JSON=$(echo "$CERT_JSON" | jq -c '{
  id: (.tbs_sha256 // "unknown"),
  issuance: {
    dns_names: .dns_names,
    tbs_sha256: .tbs_sha256,
    cert_sha256: .pubkey_sha256
  }
}')

# Send to Canary endpoint
curl -X POST "${CANARY_ENDPOINT}" \
  -H "Content-Type: application/json" \
  -d "${TRANSFORMED_JSON}" \
  --max-time 10 \
  --silent \
  --show-error

# Clean up: Remove the JSON file and the text file to prevent disk buildup
rm -f "$JSON_FILENAME"
[ -n "$TEXT_FILENAME" ] && rm -f "$TEXT_FILENAME"

exit 0
