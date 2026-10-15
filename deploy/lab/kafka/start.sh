#!/usr/bin/env bash
# SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0
#
# Starts a lab Kafka node with a dynamic KRaft quorum. The stock apache/kafka entrypoint only
# formats storage for static quorums, so this script writes server.properties, formats storage
# with the right flag and then runs Kafka in the foreground.
#
# Environment:
#   CLUSTER_ID           cluster id (22-char base64 UUID)
#   NODE_ID              node.id
#   NODE_HOST            hostname other nodes use to reach this node
#   ROLE                 controller | broker
#   QUORUM_BOOTSTRAP     controller.quorum.bootstrap.servers
#   FORMAT_MODE          initial | standalone | join   (controllers only; brokers always join)
#   INITIAL_CONTROLLERS  value for --initial-controllers when FORMAT_MODE=initial
#   EXTERNAL_PORT        host-facing listener port (brokers)
#   RACK                 broker.rack (optional)
#   REPLICATION_FACTOR   default and internal topic replication factor (brokers, default 3)
#   MIN_ISR              min.insync.replicas (brokers, default 2)
#   SASL_PORT            optional SASL_PLAINTEXT listener with SCRAM-SHA-256/512 (brokers)
#   SSL_PORT             optional SSL listener that requires client certificates (brokers);
#                        needs /certs/broker-keystore.pem and /certs/ca.pem (make lab-certs)
set -euo pipefail

: "${CLUSTER_ID:?}" "${NODE_ID:?}" "${NODE_HOST:?}" "${ROLE:?}" "${QUORUM_BOOTSTRAP:?}"

DATA_DIR=/var/lib/kafka/data
CONF=/tmp/server.properties
RF="${REPLICATION_FACTOR:-3}"
MIN_ISR="${MIN_ISR:-2}"

case "$ROLE" in
  controller)
    cat > "$CONF" <<EOF
process.roles=controller
node.id=${NODE_ID}
controller.quorum.bootstrap.servers=${QUORUM_BOOTSTRAP}
listeners=CONTROLLER://${NODE_HOST}:9093
advertised.listeners=CONTROLLER://${NODE_HOST}:9093
controller.listener.names=CONTROLLER
listener.security.protocol.map=CONTROLLER:PLAINTEXT
log.dirs=${DATA_DIR}
num.partitions=6
default.replication.factor=${RF}
min.insync.replicas=${MIN_ISR}
EOF
    ;;
  broker)
    : "${EXTERNAL_PORT:?}"
    cat > "$CONF" <<EOF
process.roles=broker
node.id=${NODE_ID}
controller.quorum.bootstrap.servers=${QUORUM_BOOTSTRAP}
listeners=INTERNAL://0.0.0.0:9092,EXTERNAL://0.0.0.0:${EXTERNAL_PORT}
advertised.listeners=INTERNAL://${NODE_HOST}:9092,EXTERNAL://localhost:${EXTERNAL_PORT}
inter.broker.listener.name=INTERNAL
controller.listener.names=CONTROLLER
listener.security.protocol.map=CONTROLLER:PLAINTEXT,INTERNAL:PLAINTEXT,EXTERNAL:PLAINTEXT
log.dirs=${DATA_DIR}
auto.create.topics.enable=false
num.partitions=6
default.replication.factor=${RF}
min.insync.replicas=${MIN_ISR}
offsets.topic.replication.factor=${RF}
transaction.state.log.replication.factor=${RF}
transaction.state.log.min.isr=${MIN_ISR}
share.coordinator.state.topic.replication.factor=${RF}
share.coordinator.state.topic.min.isr=${MIN_ISR}
group.initial.rebalance.delay.ms=0
log.segment.bytes=16777216
log.retention.hours=24
EOF
    if [[ -n "${RACK:-}" ]]; then
      echo "broker.rack=${RACK}" >> "$CONF"
    fi
    listeners="INTERNAL://0.0.0.0:9092,EXTERNAL://0.0.0.0:${EXTERNAL_PORT}"
    advertised="INTERNAL://${NODE_HOST}:9092,EXTERNAL://localhost:${EXTERNAL_PORT}"
    protocols="CONTROLLER:PLAINTEXT,INTERNAL:PLAINTEXT,EXTERNAL:PLAINTEXT"
    if [[ -n "${SASL_PORT:-}" ]]; then
      listeners+=",SASL://0.0.0.0:${SASL_PORT}"
      advertised+=",SASL://localhost:${SASL_PORT}"
      protocols+=",SASL:SASL_PLAINTEXT"
      cat >> "$CONF" <<EOF
sasl.enabled.mechanisms=SCRAM-SHA-256,SCRAM-SHA-512
listener.name.sasl.scram-sha-256.sasl.jaas.config=org.apache.kafka.common.security.scram.ScramLoginModule required;
listener.name.sasl.scram-sha-512.sasl.jaas.config=org.apache.kafka.common.security.scram.ScramLoginModule required;
EOF
    fi
    if [[ -n "${SSL_PORT:-}" && -f /certs/broker-keystore.pem ]]; then
      listeners+=",SSL://0.0.0.0:${SSL_PORT}"
      advertised+=",SSL://localhost:${SSL_PORT}"
      protocols+=",SSL:SSL"
      cat >> "$CONF" <<EOF
listener.name.ssl.ssl.keystore.type=PEM
listener.name.ssl.ssl.keystore.location=/certs/broker-keystore.pem
listener.name.ssl.ssl.truststore.type=PEM
listener.name.ssl.ssl.truststore.location=/certs/ca.pem
listener.name.ssl.ssl.client.auth=required
EOF
    elif [[ -n "${SSL_PORT:-}" ]]; then
      echo "SSL_PORT is set but /certs/broker-keystore.pem is missing; run make lab-certs" >&2
    fi
    sed -i -e "s|^listeners=.*|listeners=${listeners}|" -e "s|^advertised.listeners=.*|advertised.listeners=${advertised}|" \
      -e "s|^listener.security.protocol.map=.*|listener.security.protocol.map=${protocols}|" "$CONF"
    ;;
  *)
    echo "unknown ROLE: $ROLE" >&2
    exit 1
    ;;
esac

FORMAT_ARGS=(--no-initial-controllers)
if [[ "$ROLE" == controller ]]; then
  case "${FORMAT_MODE:-join}" in
    initial) FORMAT_ARGS=(--initial-controllers "${INITIAL_CONTROLLERS:?}") ;;
    standalone) FORMAT_ARGS=(--standalone) ;;
    join) FORMAT_ARGS=(--no-initial-controllers) ;;
    *) echo "unknown FORMAT_MODE: $FORMAT_MODE" >&2; exit 1 ;;
  esac
fi

/opt/kafka/bin/kafka-storage.sh format --cluster-id "$CLUSTER_ID" --config "$CONF" \
  --ignore-formatted "${FORMAT_ARGS[@]}"

exec /opt/kafka/bin/kafka-server-start.sh "$CONF"
