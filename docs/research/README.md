# Brokerium research knowledge base

Research date: 2026-10-15

Research notes for Brokerium ("Brokerium for Apache Kafka"), a source-available control plane and web dashboard for Apache Kafka KRaft clusters: Go server and agent, React/TypeScript dashboard, PostgreSQL. Repository: [github.com/bvaliev/brokerium](https://github.com/bvaliev/brokerium).

The notes are English translations of four research reports (A–D, originally written in Russian), regrouped by topic. Source URLs sit next to the facts they support.

> **Re-verify before relying on these facts for new Kafka versions.** Everything here is a snapshot as of 2026-10-15. Kafka releases, KIP statuses, config keys, CLI flags, error codes, Docker image behavior and third-party tool versions change quickly; check the linked sources against the Kafka version you target.

## Files

- [kafka-kraft-operations.md](kafka-kraft-operations.md): KRaft controller quorum (static vs dynamic, format flags, add/remove controller, Admin API, auto-join, static → dynamic migration, sizing), broker lifecycle (add, cordon, decommission, unregister, replace), upgrades, `kafka-features.sh`, metadata downgrade limits, KIPs 2025–2026 (report A).
- [kafka-releases-docker-reassignment-metrics.md](kafka-releases-docker-reassignment-metrics.md): releases and Java support, KAFKA-20979 and other critical bugs, official Docker images, reassignment, throttling, leader election, broker drain, large rebalances, tiered storage, metrics, security, share groups (report B).
- [market-landscape.md](market-landscape.md): comparison of Kafka management tools, newcomers, gaps no open-source tool covers end to end, Cruise Control status and limits, and the recommendation to build our own Go balancer behind a `Balancer` interface (report C, sections 1–2).
- [go-kafka-clients.md](go-kafka-clients.md): franz-go (`kadm` vs `kmsg` coverage, request routing caveats, gaps vs the Java Admin client), sarama, confluent-kafka-go (report C, section 3).
- [naming-and-trademarks.md](naming-and-trademarks.md): ASF trademark rules for "Kafka", allowed name forms, CMAK precedent, required attribution, the Brokerium naming record and pre-launch to-dos (report C, section 4, plus the naming record).
- [franz-go-controller-access.md](franz-go-controller-access.md): spike on talking to KRaft controllers directly with franz-go (seed requests work, Metadata is not served, the 41 APIs a controller listener serves, stale controller registrations in 4.3.1).
- [lab-findings.md](lab-findings.md): behavior observed in the Brokerium Docker lab on Kafka 4.3.1 while validating the runbooks (rack constraints, throttling with dead brokers, cordon gaps, stale controller observers) and what each means for Brokerium.
- [security-review-0.1.0.md](security-review-0.1.0.md): security review before the first release: what was fixed (login limits, session revocation, CSP, installer checksums), what was checked, and the accepted risks of 0.1.0.
- [licensing-research.md](licensing-research.md): the license decision (PolyForm Shield 1.0.0 + CLA), license comparison, open-core structures, precedents 2018–2026, CLA vs DCO, GitHub enforcement, acquirer due diligence, relicensing mechanics, and the AGPL recommendation that was not adopted (report D).

## Conventions

- **(unverified)** marks statements the original researchers flagged as uncertain, inferred from code, or not tested by running them.
- **"4.4"** in the Kafka notes means "merged into 4.4". On the research date the 4.4.0 release vote had passed (2026-10-13) and artifacts were being published, but there was no release announcement and no `apache/kafka:4.4.0` Docker tag.
- Dates use ISO 8601 (`YYYY-MM-DD`, or `YYYY-MM` when only the month is known).
