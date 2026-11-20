# ADR 0030: Deleting nodes and removing agents

- Status: accepted
- Date: 2026-11-20

## Context

Deleting an agent node removed its record and nothing else. Its agent stayed connected until the
stream broke; after that the server refused it as an unknown node, and the agent tried again
every 30 seconds for as long as its unit, with `Restart=always`, kept it running. A node whose
agent was not connected could be deleted while it ran a broker or a controller, and Kafka then
kept running for its cluster without anyone managing it. There was no documented way to remove
the agent from a host.

## Decision

- Deleting a node that runs a broker or a controller of a cluster is refused with
  `409 node_in_use`, whether its agent is connected or not. Decommissioning it (`drain-broker`
  with `decommission`, `remove-controller`, `replace-broker`) stops Kafka on the host and releases
  the node, which can be deleted then. Nodes without a Kafka role, unassigned or reserved for a
  cluster by their join token, are deleted. Brokerium does not decommission combined nodes
  ([ADR 0021](0021-combined-nodes.md)); they are taken out of their cluster with the Kafka tools,
  and their node is deleted once the cluster no longer has its node id as a registered broker,
  fenced or not, a voter or an observer, which the server asks the cluster when the node is
  deleted.
- Deleting a node ends the gateway session of its agent with `NOT_FOUND`. The gateway remembers
  the nodes deleted since it started and refuses sessions of their agents that authenticated just
  before the deletion. A connection or enrollment of a node that does not exist is answered with
  `NOT_FOUND`; when the node cannot be read the answer is `UNAVAILABLE`; wrong credentials keep
  `PERMISSION_DENIED`.
- With mutual TLS the status comes from the server the agent verified, so the agent trusts it.
  Statuses of the insecure transport ([ADR 0017](0017-agent-transport-behind-proxies.md)) are not
  authenticated, and anyone on the path could stop the agent with a forged one. The server
  therefore answers the session hello of a node that does not exist with `NodeDeleted`, a
  signature of the CA key over `"brokerium-node-deleted-v1"`, the node id and the agent nonce of
  the hello, and then ends the stream with `NOT_FOUND`. The agent stops only on a valid signature;
  the status alone, or a signature of another key, makes it connect again. The server signs
  without checking the hello, because the secret of a deleted node is gone; the domain keeps the
  signature apart from all other inputs, and the nonce, new for every hello, makes it worthless
  for any other hello.
- The agent logs once that its node was deleted and exits with status 6, the LSB status "program
  is not configured" (`agenthost.ExitNodeDeleted`). The unit of the install script keeps
  `Restart=always` and adds `RestartPreventExitStatus=6`, so systemd does not start it again and
  shows it as failed. A lost connection, a server restart and every other error still make the
  agent connect again. A Kafka of its own unit keeps running; a Kafka that runs as a child process
  of the agent stops with it.
- `GET /api/v1/agent/uninstall.sh`, next to the install script, serves a script run as root that
  removes the agent unit, binary, sudo rule, environment file, state and account. It changes
  nothing while `brokerium-kafka.service` is active unless given `--stop-kafka`, because Kafka
  runs from the configuration in the agent state; the unit of Kafka goes with the agent.
  `--remove-kafka` removes the Kafka releases and `--delete-data` the data directory from the
  environment file of the agent. It can run again at any time. `DELETE /api/v1/nodes/{id}`
  answers `200` with the command that runs it instead of `204`, and the dashboard shows that
  command with its options.
- The install and uninstall scripts and the agent take the paths and units of a host from one
  package, `internal/agenthost`.

## Consequences

- Hosts leave Brokerium in two steps: delete the node, then run the uninstall script on the host.
- Agents before 0.3.2 keep connecting after their node was deleted. Units written by earlier
  install scripts restart an agent of 0.3.2 that exits with 6, every five seconds, until the
  uninstall script removes it or the install command runs again.
- A node released while its agent was not connected still has to stop Kafka when the agent comes
  back. Deleting it drops that stop: the agent starts Kafka after a reboot if it ran before, and
  then exits. The uninstall script stops it only with `--stop-kafka`.
- A node whose Kafka node never joined its cluster, after an operation failed, keeps its role and
  cannot be deleted; the operation continues when the host is chosen again
  ([ADR 0018](0018-explicit-node-assignment.md)), or deleting the registration of the cluster
  releases it.
