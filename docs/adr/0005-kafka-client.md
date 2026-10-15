# ADR 0005: franz-go with raw KRaft requests

- Status: accepted
- Date: 2026-10-15

## Context

The control plane needs the full Kafka admin protocol, including KRaft-specific requests:
DescribeQuorum, AddRaftVoter, RemoveRaftVoter, UnregisterBroker, DescribeCluster with fenced
brokers, UpdateFeatures. sarama lacks most of them and confluent-kafka-go requires CGO and
does not support partition reassignment.

## Decision

- Use **franz-go** (`kgo`, `kadm`, `kmsg`; BSD-3-Clause).
- Use `kadm` where it covers the feature (topics, configs, ACLs, SCRAM, quotas, groups,
  reassignments, log dirs, leader election).
- Implement KRaft operations in `internal/kafka` as thin wrappers over `kmsg` requests.
  Requests are sent to brokers, which forward controller-bound requests to the active
  controller. Direct controller connections (KIP-919 `bootstrap.controllers`) are not
  supported by franz-go and are a separate spike.

## Consequences

- One library for the whole protocol, no CGO.
- The library has a single main maintainer; the wrappers isolate the rest of the code from it.
