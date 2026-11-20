#!/usr/bin/env bash
# SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0
#
# Starts a lab Kafka broker in ZooKeeper mode: writes server.properties and runs Kafka in the
# foreground. Used by the clusters delta and epsilon with the image of deploy/lab/kafka/Dockerfile.
#
# Environment:
#   BROKER_ID            broker.id
#   NODE_HOST            hostname the other brokers use to reach this broker
#   ZOOKEEPER_CONNECT    zookeeper.connect
#   EXTERNAL_PORT        host-facing listener port
#   RACK                 broker.rack (optional)
#   REPLICATION_FACTOR   default and internal topic replication factor (default 3)
#   MIN_ISR              min.insync.replicas (default 2)
set -euo pipefail

: "${BROKER_ID:?}" "${NODE_HOST:?}" "${ZOOKEEPER_CONNECT:?}" "${EXTERNAL_PORT:?}"

CONF=/tmp/server.properties
RF="${REPLICATION_FACTOR:-3}"
MIN_ISR="${MIN_ISR:-2}"

# ACLs are enforced once defined; without any ACL access stays open (lab only).
cat > "$CONF" <<EOF
broker.id=${BROKER_ID}
zookeeper.connect=${ZOOKEEPER_CONNECT}
listeners=INTERNAL://0.0.0.0:9092,EXTERNAL://0.0.0.0:${EXTERNAL_PORT}
advertised.listeners=INTERNAL://${NODE_HOST}:9092,EXTERNAL://localhost:${EXTERNAL_PORT}
inter.broker.listener.name=INTERNAL
listener.security.protocol.map=INTERNAL:PLAINTEXT,EXTERNAL:PLAINTEXT
log.dirs=/var/lib/kafka/data
auto.create.topics.enable=false
num.partitions=6
default.replication.factor=${RF}
min.insync.replicas=${MIN_ISR}
offsets.topic.replication.factor=${RF}
transaction.state.log.replication.factor=${RF}
transaction.state.log.min.isr=${MIN_ISR}
group.initial.rebalance.delay.ms=0
log.segment.bytes=16777216
log.retention.hours=24
authorizer.class.name=kafka.security.authorizer.AclAuthorizer
allow.everyone.if.no.acl.found=true
super.users=User:ANONYMOUS
EOF
if [[ -n "${RACK:-}" ]]; then
  echo "broker.rack=${RACK}" >> "$CONF"
fi

exec /opt/kafka/bin/kafka-server-start.sh "$CONF"
