#!/usr/bin/env bash
# SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0
#
# Agent-on-VM lab: brokerium-server in a container (image built from this repository) and
# systemd hosts that install brokerium-agent with the real install.sh. Used by TestAgentVMs.
#
#   deploy/lab/vm/vm.sh up [host...]   default hosts: vm-lab-c1 vm-lab-b101
#   deploy/lab/vm/vm.sh down
set -euo pipefail

cd "$(dirname "$0")"
ROOT="$(cd ../../.. && pwd)"
NET=brokerium-lab
SERVER=brokerium-vm-server
PG=brokerium-lab-postgres-1

case "${1:-}" in
  up)
    shift
    hosts=("$@")
    [[ ${#hosts[@]} -gt 0 ]] || hosts=(vm-lab-c1 vm-lab-b101)
    docker build -q -t brokerium-vm . >/dev/null
    docker build -q -f "$ROOT/deploy/docker/server.Dockerfile" -t bvaliev/brokerium-server:dev "$ROOT" >/dev/null
    (cd "$ROOT" && GOOS=linux GOARCH="$(docker info --format '{{.Architecture}}' | sed 's/x86_64/amd64/;s/aarch64/arm64/')" \
      CGO_ENABLED=0 mise exec -- go build -o .data/vm/brokerium-agent ./cmd/brokerium-agent)
    # A fresh lab: new server data (and secret key) and a new database.
    docker rm -f "$SERVER" >/dev/null 2>&1 || true
    docker volume rm -f "$SERVER-data" >/dev/null 2>&1 || true
    docker exec "$PG" psql -U brokerium -d brokerium -c "DROP DATABASE IF EXISTS brokerium_vm" >/dev/null
    docker exec "$PG" psql -U brokerium -d brokerium -c "CREATE DATABASE brokerium_vm" >/dev/null
    docker run -d --name "$SERVER" --hostname "$SERVER" --network "$NET" -p 18080:8080 -v "$SERVER-data:/var/lib/brokerium" \
      -e BROKERIUM_DATABASE_URL="postgres://brokerium:brokerium@postgres:5432/brokerium_vm?sslmode=disable" \
      -e BROKERIUM_ADMIN_PASSWORD=brokerium-admin -e BROKERIUM_PUBLIC_URL="http://$SERVER:8080" \
      -e BROKERIUM_AGENT_ENDPOINT="$SERVER:8443" -e BROKERIUM_AGENT_SERVER_NAMES="$SERVER" \
      -e BROKERIUM_SNAPSHOT_INTERVAL=10s bvaliev/brokerium-server:dev >/dev/null
    for h in "${hosts[@]}"; do
      docker rm -f "$h" >/dev/null 2>&1 || true
      docker run -d --name "$h" --hostname "$h" --network "$NET" --privileged --cgroupns=host \
        -v /sys/fs/cgroup:/sys/fs/cgroup:rw --tmpfs /run --tmpfs /run/lock --memory 1g brokerium-vm >/dev/null
      docker cp "$ROOT/.data/vm/brokerium-agent" "$h:/opt/brokerium-agent"
    done
    echo "server: http://localhost:18080 (admin / brokerium-admin), hosts: ${hosts[*]}"
    ;;
  down)
    docker ps -a --format '{{.Names}}' | grep -E "^($SERVER|vm-lab-.*)$" | xargs -r docker rm -f >/dev/null
    docker volume rm -f "$SERVER-data" >/dev/null 2>&1 || true
    docker exec "$PG" psql -U brokerium -d brokerium -c "DROP DATABASE IF EXISTS brokerium_vm" >/dev/null || true
    ;;
  *)
    echo "usage: $0 up [host...] | down" >&2
    exit 2
    ;;
esac
