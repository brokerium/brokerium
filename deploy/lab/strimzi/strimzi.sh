#!/usr/bin/env bash
# SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0
#
# Strimzi lab: a kind cluster with the Strimzi operator and the Kafka cluster "lab".
# Uses its own kubeconfig (.data/kind.kubeconfig) and never touches ~/.kube/config.
#
#   deploy/lab/strimzi/strimzi.sh up | down | status
set -euo pipefail

cd "$(dirname "$0")"
STRIMZI_VERSION="${STRIMZI_VERSION:-1.3.0}"
export KUBECONFIG="$(cd ../../.. && pwd)/.data/kind.kubeconfig"
mkdir -p "$(dirname "$KUBECONFIG")"

case "${1:-}" in
  up)
    kind get clusters 2>/dev/null | grep -qx brokerium || kind create cluster --config kind.yaml --kubeconfig "$KUBECONFIG"
    kubectl create namespace kafka --dry-run=client -o yaml | kubectl apply -f -
    curl -fsSL "https://github.com/strimzi/strimzi-kafka-operator/releases/download/${STRIMZI_VERSION}/strimzi-cluster-operator-${STRIMZI_VERSION}.yaml" |
      sed 's/namespace: .*/namespace: kafka/' | kubectl apply -n kafka -f - >/dev/null
    kubectl -n kafka rollout status deployment/strimzi-cluster-operator --timeout=600s
    kubectl apply -f kafka.yaml
    kubectl -n kafka wait kafka/lab --for=condition=Ready --timeout=1200s
    echo "Kafka lab is ready: bootstrap localhost:32100, kubeconfig $KUBECONFIG"
    ;;
  down)
    kind delete cluster --name brokerium
    ;;
  status)
    kubectl -n kafka get kafka,kafkanodepools,pods
    ;;
  *)
    echo "usage: $0 up | down | status" >&2
    exit 2
    ;;
esac
