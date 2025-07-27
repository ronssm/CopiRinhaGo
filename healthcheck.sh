#!/bin/sh

set -e

if ! wget --spider -q --timeout=2 --tries=1 http://127.0.0.1:9999/payments-summary 2>/dev/null; then
    echo "Payments summary endpoint not responding"
    exit 1
fi

echo "Health check passed"
exit 0
