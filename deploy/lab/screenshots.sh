#!/usr/bin/env bash
# SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0
#
# Captures the dashboard screenshots in docs/images from the lab with demo data: cluster alpha
# with traffic and two new, still empty brokers, and cluster beta. A separate server with its own
# database runs on port 18090, so a development server on 8080 keeps running.
#
# Cluster alpha is recreated from scratch: the volumes of its nodes are deleted first.
#
# Usage: make screenshots   (builds bin/brokerium-server first)
set -euo pipefail

cd "$(dirname "$0")/../.."
lab=deploy/lab/lab.sh
port=${SCREENSHOT_PORT:-18090}
url=http://localhost:$port
database=brokerium_screenshots
data=.data/screenshots
password=screenshots

"$lab" rm alpha-c1 alpha-c2 alpha-c3 alpha-c4 alpha-c5 \
  alpha-b1 alpha-b2 alpha-b3 alpha-b4 alpha-b5 alpha-b6
"$lab" up alpha beta
"$lab" load alpha 20000
"$lab" up alpha-scale
docker compose -f deploy/lab/compose.yaml exec -T postgres psql -q -U brokerium -d postgres \
  -c "DROP DATABASE IF EXISTS $database" -c "CREATE DATABASE $database"

rm -rf "$data"
mkdir -p "$data"
BROKERIUM_HTTP_ADDR=:$port \
  BROKERIUM_GRPC_ADDR=:$((port + 1)) \
  BROKERIUM_DATA_DIR=$data \
  BROKERIUM_DATABASE_URL="postgres://brokerium:brokerium@localhost:55432/$database?sslmode=disable" \
  BROKERIUM_ADMIN_PASSWORD=$password \
  BROKERIUM_SNAPSHOT_INTERVAL=5s \
  ./bin/brokerium-server >"$data/server.log" 2>&1 &
server=$!
trap 'kill "$server" 2>/dev/null || true' EXIT
for _ in $(seq 1 60); do curl -fs "$url/readyz" >/dev/null && break; sleep 1; done

api() {
  curl -fsS -b "$data/cookies" -c "$data/cookies" -H 'Content-Type: application/json' \
    -H 'X-Brokerium-Request: 1' "$@"
}
api -d "{\"username\":\"admin\",\"password\":\"$password\"}" "$url/api/v1/auth/login" >/dev/null
api -d '{"name":"alpha","bootstrapServers":["localhost:19101","localhost:19102","localhost:19103"]}' \
  "$url/api/v1/clusters" >/dev/null
api -d '{"name":"beta","bootstrapServers":["localhost:29101"]}' "$url/api/v1/clusters" >/dev/null

# Steady traffic for about five minutes, so the dashboard shows rates while it is captured.
"$lab" cli kafka-producer-perf-test.sh --topic orders --num-records 90000 --record-size 1024 \
  --throughput 300 --bootstrap-server alpha-b1:9092 >/dev/null 2>&1 &
sleep 45

cd web
BROKERIUM_URL=$url BROKERIUM_PASSWORD=$password \
  mise exec -- pnpm exec playwright test --config playwright.screenshots.config.ts
