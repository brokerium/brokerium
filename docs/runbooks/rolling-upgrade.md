# Rolling upgrade and finalization (scenario 8)

Validated on 2026-10-15: a Docker lab cluster with 3 controllers and 3 brokers went from Kafka
4.3.1 to 4.4.0 through Brokerium, then `metadata.version` was finalized from 4.3-IV0 to 4.4-IV2
(`share.version` 1 to 2). Records written before the upgrade were read back afterwards.
Automated as `TestRollingUpgrade` in [test/e2e/upgrade_test.go](../../test/e2e/upgrade_test.go).

## What Brokerium checks before it plans

| Check | Rule | Source |
| --- | --- | --- |
| Version order | The target is newer than the running version. | Kafka upgrade notes |
| Upgrade path | Kafka 4.x needs a KRaft cluster on 3.3 or later; upgrade to 3.9 first otherwise. | [Kafka upgrade notes](https://kafka.apache.org/43/getting-started/upgrade/) |
| Java | Every agent reports its Java runtime; brokers and controllers of Kafka 4.x need Java 17 or later. | [Kafka Java versions](https://kafka.apache.org/43/operations/java-version/) |
| Known defects | KAFKA-20979 affects 4.1.0 up to 4.3.1 (no fix in 4.2.x): the plan warns for both the current and the target version. Prefer 4.3.2 or 4.4.0. | [KAFKA-20979](https://issues.apache.org/jira/browse/KAFKA-20979) |
| Health | The health gate runs before the first node and after every node. | [ADR 0008](../adr/0008-operations-engine.md) |

## Order of steps

1. Health gate.
2. Download the release on every node first (SHA-512 checked against the checksum file of
   Apache), so a failed download stops the plan before any restart. Agents try
   `BROKERIUM_KAFKA_MIRROR` first when it is set
   ([internal mirror](../guide/install.md#kafka-releases-from-an-internal-mirror)).
3. For each node, controllers first and the quorum leader last, then brokers: write the
   configuration for the new version, stop gracefully, start on the new version, wait until
   the node is back (broker registered and unfenced, controller caught up), health gate.
4. Save the new version in the cluster settings and run a preferred leader election.

`metadata.version` stays on the old level, so the binaries can still be rolled back to the
previous version.

## Finalization

`finalize-upgrade` raises every feature to the highest level all nodes support
(`metadata.version`, `share.version`, `group.version`, ...). It is irreversible and asks for the
cluster name. `kraft.version` is not touched there; a static quorum is migrated with
`migrate-quorum`.

In the dashboard: cluster page, tab **Upgrade**: enter the version, preview the plan, confirm;
after checking the cluster on the new binaries, press **Finalize** and type the cluster name.

## Rollback

- Before finalization the previous release can still run on the same data, because
  `metadata.version` was not raised. Brokerium does not automate downgrades yet: a
  `rolling-upgrade` only accepts newer versions and `rolling-restart` keeps each node on its
  active version. A downgrade operation is on the roadmap.
- After finalization `metadata.version` generally cannot be lowered without losing metadata;
  keep the new version or restore from backups.
