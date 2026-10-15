# Runbooks

Procedures validated by hand in the Docker lab on Kafka 4.3.1 (2026-10-15). Each runbook lists the
exact commands and what was observed. Brokerium automates these procedures as operations; the
runbooks are the reference behavior for the operations and for the e2e tests.

| Scenario | Runbook |
| --- | --- |
| 1. Deploy cluster alpha and generate load | [deploy/lab/README.md](../../deploy/lab/README.md) |
| 2. Add two brokers and rebalance with throttling | [scale-out-brokers.md](scale-out-brokers.md) |
| 3. Grow the controller quorum to 5 and back to 3 | [controller-quorum.md](controller-quorum.md) |
| 4. Replace a failed broker | [replace-broker.md](replace-broker.md) |
| 5. Decommission a broker | [decommission-broker.md](decommission-broker.md) |
| 10. Migrate a static quorum to a dynamic one | [static-to-dynamic-quorum.md](static-to-dynamic-quorum.md) |

Scenarios 6-9 (one-click balancing, multi-cluster dashboard, rolling upgrade, data plane) are
product features and are validated through Brokerium itself.

All commands run from `deploy/lab/`; `./lab.sh cli <tool>` runs a Kafka CLI tool inside the
`tools` container on the lab network.
