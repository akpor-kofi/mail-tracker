#!/bin/sh
set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$repo_root/apps/api"

for domain in mailboxes correspondence tracking; do
	go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.8.0 \
		-config "oapi-codegen.$domain.yaml" \
		../../packages/api-contract/openapi.yaml
done
