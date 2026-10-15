#!/usr/bin/env bash
# SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0
#
# Brokerium lab helper. Usage: deploy/lab/lab.sh <command> [args]
#
#   up [profile...]           start PostgreSQL and the given profiles (default: alpha)
#   start <service...>        start or recreate individual services
#   stop <service...>         stop services (simulate a node failure)
#   rm <service...>           stop and remove services and their volumes (simulate disk loss)
#   down                      stop all lab containers, keep volumes
#   reset                     stop all lab containers and delete all lab volumes
#   ps                        list lab containers
#   logs [service...]         follow logs
#   cli <tool> [args...]      run a Kafka CLI tool in the tools container, e.g.
#                             cli kafka-topics.sh --bootstrap-server alpha-b1:9092 --list
#   exec <service> [cmd...]   run a command inside a lab container
#   quorum [cluster]          describe the KRaft quorum (default: alpha)
#   load [cluster] [records]  create demo topics and produce records (default: alpha 200000)
#   dist [cluster]            replicas and leaders per broker (user topics)
#   urp [cluster]             under-replicated partitions
#   put <path>                write stdin to <path> inside the tools container
set -euo pipefail

cd "$(dirname "$0")"

ALL_PROFILES=(alpha alpha-scale alpha-quorum alpha-replace beta gamma tools)
KAFKA_BIN=/opt/kafka/bin

compose() {
  local args=()
  for p in "${ALL_PROFILES[@]}"; do args+=(--profile "$p"); done
  docker compose -f compose.yaml "${args[@]}" "$@"
}

bootstrap() {
  case "$1" in
    alpha) echo "alpha-b1:9092,alpha-b2:9092" ;;
    beta) echo "beta-b1:9092" ;;
    gamma) echo "gamma-b1:9092" ;;
    *) echo "unknown cluster: $1" >&2; exit 1 ;;
  esac
}

replication_factor() {
  case "$1" in
    alpha) echo 3 ;;
    *) echo 1 ;;
  esac
}

cli() {
  local tool=$1
  shift
  compose exec -T tools "$KAFKA_BIN/$tool" "$@"
}

cmd=${1:-help}
shift || true

case "$cmd" in
  up)
    [[ $# -eq 0 ]] && set -- alpha
    args=()
    for p in "$@"; do args+=(--profile "$p"); done
    docker compose -f compose.yaml "${args[@]}" up -d --wait
    ;;
  start)
    compose up -d --wait "$@"
    ;;
  stop)
    compose stop "$@"
    ;;
  rm)
    compose rm --stop --force "$@"
    for svc in "$@"; do
      docker volume rm -f "brokerium-lab_${svc}" >/dev/null
    done
    ;;
  down)
    compose down --remove-orphans
    ;;
  reset)
    compose down --volumes --remove-orphans
    ;;
  ps)
    compose ps --format 'table {{.Service}}\t{{.State}}\t{{.Status}}\t{{.Ports}}'
    ;;
  logs)
    compose logs -f --tail=100 "$@"
    ;;
  cli)
    cli "$@"
    ;;
  exec)
    svc=$1
    shift
    compose exec -T "$svc" "$@"
    ;;
  quorum)
    cluster=${1:-alpha}
    cli kafka-metadata-quorum.sh --bootstrap-server "$(bootstrap "$cluster")" describe --status
    cli kafka-metadata-quorum.sh --bootstrap-server "$(bootstrap "$cluster")" describe --replication
    ;;
  load)
    cluster=${1:-alpha}
    records=${2:-200000}
    bs=$(bootstrap "$cluster")
    rf=$(replication_factor "$cluster")
    for spec in orders:12 payments:6 clicks:24; do
      topic=${spec%%:*}
      partitions=${spec##*:}
      cli kafka-topics.sh --bootstrap-server "$bs" --create --if-not-exists \
        --topic "$topic" --partitions "$partitions" --replication-factor "$rf"
    done
    for topic in orders payments clicks; do
      cli kafka-producer-perf-test.sh --topic "$topic" --num-records "$records" \
        --record-size 1024 --throughput -1 --bootstrap-server "$bs" \
        --command-property acks=all linger.ms=20 batch.size=262144 | tail -1
    done
    ;;
  dist)
    cluster=${1:-alpha}
    cli kafka-topics.sh --bootstrap-server "$(bootstrap "$cluster")" --describe --exclude-internal |
      awk '/Partition:/ {
             for (i = 1; i <= NF; i++) { if ($i == "Leader:") l = $(i + 1); if ($i == "Replicas:") r = $(i + 1) }
             leaders[l]++; n = split(r, a, ","); for (j = 1; j <= n; j++) reps[a[j]]++
           }
           END { for (b in reps) printf "broker %s replicas=%d leaders=%d\n", b, reps[b], leaders[b] }' | sort
    ;;
  urp)
    cluster=${1:-alpha}
    cli kafka-topics.sh --bootstrap-server "$(bootstrap "$cluster")" --describe --under-replicated-partitions
    ;;
  put)
    compose exec -T tools bash -c "cat > '$1'"
    ;;
  help | *)
    sed -n '4,24p' "$0"
    ;;
esac
