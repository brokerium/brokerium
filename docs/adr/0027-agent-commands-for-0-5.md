# ADR 0027: Agent commands for files, artifacts, host facts and autostart

- Status: accepted
- Date: 2026-11-20
- Extends: [ADR 0009](0009-agent-protocol.md)

## Context

The agent runs a closed set of typed commands: install Kafka, write the configuration and the
heap, format the storage, start, stop, report the status, wipe the data. Configuration, security,
the JMX exporter, bounded log files and host checks need a few more, and secrets must stay
confidential even on the insecure agent transport ([ADR 0017](0017-agent-transport-behind-proxies.md)).

## Decision

- **Capabilities.** The agent announces what it supports in its hello message. The server
  refuses a feature on a node whose agent lacks it and asks to upgrade the agent.
- **Sealed payloads.** The agent creates an X25519 key pair in its state directory when it
  enrolls and reports the public key. Secret payloads are encrypted for that key (NaCl sealed box)
  and decrypted only by the agent.
- **WriteFiles.** Writes files below the agent's managed directory for Kafka: TLS material, JAAS
  files, the logging configuration, the exporter configuration. Names are relative and checked;
  files are written atomically with mode `0640` and the group `kafka`; content may be sealed.
- **InstallArtifact.** Downloads a jar with a SHA-256 checksum into the agent's directory, from
  the mirror in `BROKERIUM_KAFKA_MIRROR` when one is set. The first artifact is the Prometheus JMX
  exporter javaagent.
- **WriteConfig** also carries `KAFKA_OPTS` for the javaagent (only a jar the agent installed,
  with a host, a port and a configuration file it wrote) and `KAFKA_LOG4J_OPTS` for the logging
  configuration it wrote. `agentcfg` validates both like the heap.
- **Bounded logs.** Managed nodes get a logging configuration that deletes rolled files by count
  and age: log4j2 for Kafka 4.x, log4j for 3.x. Limits default to 7 days and 100 files per log
  and are part of the cluster configuration ([ADR 0025](0025-kafka-configuration.md)).
- **Autostart.** `SetAutostart` enables `brokerium-kafka.service` while the node runs and
  disables it when Brokerium stops the node; the install script grants exactly these two `systemctl`
  calls. The agent starts Kafka after a reboot before it enrolls, so an expired certificate does
  not keep Kafka down.
- **Host facts.** Every few minutes the status carries the OS and kernel, memory, swap,
  transparent huge pages, `net.core.rmem_max`, `net.core.wmem_max`, `vm.max_map_count`,
  `vm.swappiness`, the open-file and process limits of the Kafka process, whether its unit is
  enabled, the Kafka jars in the installation, the size and count of the Kafka log files and the
  Java version, read again after changes.
- **Edition.** The commands serve both editions. Autostart and bounded logs are Community fixes;
  sealed payloads, files and artifacts are used by the Enterprise features `security` and
  `monitoring`; the checks on host facts belong to `problems`.

## Consequences

- Agents older than 0.5.0 keep working with the commands they know; features that need the new
  ones say which agent version they need.
- Existing hosts run the install script again to get the new sudo rules.
- Secrets never cross the transport in clear text, whatever the transport is.
