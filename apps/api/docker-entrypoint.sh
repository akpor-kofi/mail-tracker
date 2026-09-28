#!/bin/sh
set -eu

# Railway volumes mount at runtime, after the image's build-time chown.
chown app:app "${ATTACHMENT_DIR:-/app/data}"
exec su-exec app /app/mail-tracker
