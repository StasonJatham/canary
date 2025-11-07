#!/bin/sh
# Wrapper script to run certspotter continuously and clean up files

set -u  # Exit on undefined variable

SLEEP_INTERVAL=15  # Poll every 15 seconds for new certificates
MAX_FILES=1000     # Maximum files to keep before cleanup
CERT_DIR="/root/.certspotter/certs"
STATE_DIR="/var/lib/certspotter"

# Signal handling to ensure graceful shutdown
trap 'echo "[$(date)] Received signal, shutting down gracefully..."; exit 0' TERM INT

echo "[$(date)] Starting continuous certspotter monitoring..."
echo "[$(date)] Poll interval: ${SLEEP_INTERVAL}s"
echo "[$(date)] Certificate directory: ${CERT_DIR}"
echo "[$(date)] State directory: ${STATE_DIR}"

# Ensure state directory exists
mkdir -p "${STATE_DIR}"

ITERATION=0
FIRST_RUN=true

while true; do
    ITERATION=$((ITERATION + 1))
    echo "[$(date)] === Iteration $ITERATION - Starting certspotter ==="

    # On first run, use -start_at_end to skip historical certificates
    # On subsequent runs, certspotter resumes from its saved state
    if [ "$FIRST_RUN" = true ]; then
        echo "[$(date)] First run - skipping historical certificates"
        certspotter -watchlist /watchlist.txt -script /webhook.sh -start_at_end || {
            EXIT_CODE=$?
            echo "[$(date)] Certspotter failed with exit code: $EXIT_CODE"
        }
        FIRST_RUN=false
    else
        certspotter -watchlist /watchlist.txt -script /webhook.sh || {
            EXIT_CODE=$?
            echo "[$(date)] Certspotter failed with exit code: $EXIT_CODE"
        }
    fi

    echo "[$(date)] Certspotter scan complete"

    # Clean up old certificate files if too many exist
    if [ -d "$CERT_DIR" ]; then
        FILE_COUNT=$(find "$CERT_DIR" -type f 2>/dev/null | wc -l || echo "0")
        if [ "$FILE_COUNT" -gt "$MAX_FILES" ]; then
            echo "[$(date)] Cleaning up old files (found: $FILE_COUNT, max: $MAX_FILES)"
            find "$CERT_DIR" -type f -mmin +10 -delete 2>/dev/null || true
            REMAINING=$(find "$CERT_DIR" -type f 2>/dev/null | wc -l || echo "0")
            echo "[$(date)] Cleanup complete. Files remaining: $REMAINING"
        fi
    fi

    # Wait before next poll
    echo "[$(date)] Waiting ${SLEEP_INTERVAL}s before next check..."
    sleep $SLEEP_INTERVAL &
    wait $!  # Wait for sleep to complete, allows trap to interrupt
done
