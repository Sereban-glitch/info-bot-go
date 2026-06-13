#!/bin/bash
# Start info-bot-go
if [ -f .env ]; then
  set -a
  source .env
  set +a
fi
exec go run .
