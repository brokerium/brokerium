# ADR 0012: Strimzi provider through the Kubernetes REST API

- Status: accepted
- Date: 2026-10-15

## Context

On Kubernetes, the Strimzi operator owns pods, storage, certificates and the controller quorum
of Kafka clusters. Brokerium must not fight the operator, but it still has to scale brokers,
restart nodes and upgrade Kafka with the same safety rules as on virtual machines
([ADR 0008](0008-operations-engine.md)). Strimzi 1.x serves only the `v1` versions of its
custom resources; older releases serve `v1beta2`.

## Decision

- The `strimzi` provider talks to the Kubernetes API over plain REST
  (`internal/provider/strimzi`), not through client-go. It needs a handful of verbs on three
  resource types; client-go would add a large dependency tree for that.
- Credentials: the service account of the Brokerium pod (in-cluster, with the ClusterRole from
  the Helm chart), or a kubeconfig stored as a sealed cluster secret. Kubeconfigs with a bearer
  token or a client certificate are accepted. Exec credential plugins are rejected, because the
  server never runs binaries named by user input.
- API versions of `kafka.strimzi.io` and `core.strimzi.io` are discovered at connection time,
  so the same code works with `v1` and `v1beta2`.
- Kafka-level work stays on the Kafka protocol: health gates, reassignments and throttles run
  exactly as for other providers, through a bootstrap address reachable from the server.
- Node pools (`KafkaNodePool`) are the unit of scaling:
  - scale up: raise `spec.replicas`, wait until the pool is ready, then rebalance with the
    Brokerium planner;
  - scale down: evacuate the highest node ids with the planner and throttled reassignments,
    then lower `spec.replicas` with the `strimzi.io/remove-node-ids` annotation.
  Cruise Control is not required.
- Rolling restart sets `strimzi.io/manual-rolling-update` on the StrimziPodSet of each pool,
  controllers first, one pool at a time, with a health gate in between.
- Upgrades set `spec.kafka.version` while pinning `spec.kafka.metadataVersion` to the current
  level, because the operator raises the metadata version on its own otherwise. Raising
  `metadataVersion` is the separate, irreversible finalization step.
- Controller quorum changes are not offered for Strimzi clusters: the operator owns the quorum.

## Consequences

- No client-go in the dependency tree; the provider is small and its fake API in tests is a
  plain `httptest` server.
- Kubeconfigs that depend on cloud CLIs (`aws eks get-token`, `gke-gcloud-auth-plugin`) need a
  service account token instead, or Brokerium running in the same cluster.
- The provider covers what Strimzi exposes; anything else (storage resizing, listeners) is
  still configured in the Strimzi resources directly.
