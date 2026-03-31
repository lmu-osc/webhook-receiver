#!/bin/sh
set -eu

# Ensure the mounted repo path exists and is writable by the app user.
mkdir -p /app/repo
chown -R app:app /app/repo

exec su-exec app /app/webhook-receiver
