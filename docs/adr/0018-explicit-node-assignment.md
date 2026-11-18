# ADR 0018: Explicit choice of agent hosts

- Status: accepted
- Date: 2026-10-20

## Context

Brokerium tied agents to the nodes of a cluster by name. Node 3 of cluster `prod` had to run on
an agent named `prod-c3` if it was a controller and broker 101 on `prod-b101`. Creating a
cluster, adding brokers or a controller and replacing a broker waited for an agent with the
expected name, and the listeners, the quorum bootstrap servers and the static voters used that
name as the host. Operators had to name hosts after a cluster that did not exist yet, the names
had to resolve in DNS, and a host could take another role or join another cluster only after it
was enrolled again under another name. Kafka needs none of this: node ids, roles and host names
are independent of each other.

## Decision

- An agent node is a host with any name. A join token may name the node; without a name the
  node is named after the host name its agent reports at enrollment, with `-2`, `-3` and so on
  when that name is taken, and characters outside `[A-Za-z0-9._-]` become `-`. A join token for
  a node id enrolls that node again: it keeps its id, name and assignment.
- Every node has an advertised host, which the listeners of its Kafka node advertise and which
  the quorum and the server use. It starts as the host name the agent reported and changes with
  `PATCH /api/v1/nodes/{id}`, together with the name and a rack. A host whose advertised host is
  neither a DNS name nor an IP address cannot be chosen, and a node that runs Kafka keeps a valid
  one. The rack is the default `broker.rack` of a broker on the host.
- Operations on agent clusters name their hosts. `bootstrap-cluster` takes
  `nodes: [{node, role, nodeId?, rack?}]`, `add-broker` takes `nodes`, `add-controller` takes
  `node`, and `replace-broker` takes `node`, which for `same-id` defaults to the host of the
  broker. Node ids default to 1, 2, ... for controllers and 101, 102, ... for brokers in the
  given order when a cluster is created, and to the next free id after the highest one of the
  cluster when nodes are added. Docker clusters keep their counts and container names.
- A host can be chosen when its agent is connected, it has no Kafka role, it belongs to no other
  cluster, its advertised host is valid and not advertised by another chosen host or by a node
  that runs Kafka in the cluster, it does not run an adopted Kafka, and its log directory is not
  formatted, or formatted for the same Kafka cluster and node id. Plans refuse other hosts with a
  message that names the host. Steps check again when they run: a first step checks all chosen
  hosts before anything changes, and the step that waits for the agent gives the host its role
  in one conditional update, which fails when another operation took the host meanwhile or
  another node of the cluster has the id.
- An operation that fails can leave hosts with a role whose Kafka node never joined the cluster.
  Choosing such a host again for `add-broker` (its broker is not registered) or `add-controller`
  (its controller is not a voter) continues that node instead of refusing the host.
- Steps address agent nodes by their id, so a node can be renamed at any time. Removing a node
  from an agent cluster (decommissioning a broker, removing a controller, replacing a broker)
  stops Kafka on the host and releases it: its role, node id and cluster are cleared. A host
  whose agent is not connected is released at once and marked, and the server stops Kafka on it
  when its agent connects again: the agent starts Kafka after a reboot when it ran before, and
  the host must not run the node next to the host that took it over. Deleting the registration
  of a cluster releases its hosts as well and leaves Kafka running on them.
- The role `combined`, broker and controller in one process, is allowed by the schema;
  operations refuse it until it is supported.

## Consequences

- Hosts need no naming scheme, can be enrolled before their cluster exists, and one fleet of
  hosts serves several clusters over time.
- The advertised host must resolve for clients, the other nodes and the server. A changed
  advertised host of a node that runs Kafka takes effect when the node is configured again by a
  rolling restart or upgrade, which then records the new bootstrap servers. Clusters with adopted
  nodes keep their recorded addresses, because Brokerium does not render their listeners.
- A released host keeps its Kafka data. Its data directory has to be cleared before it can run
  a node of another cluster or another node id; plans refuse it until then.
- A failed creation of a cluster cannot be continued: the operator deletes the registration,
  which releases the hosts, and stops Kafka and clears the data directories on them.
- Re-enrolling a host, for example to switch the agent transport, needs a join token for its
  node; a token without a node enrolls the host as a new node.
- The dashboard offers the hosts that are free for a cluster.

## Migration of existing clusters

Migration 0005 adds `nodes.advertised_host`, `nodes.rack`, `nodes.kafka_stop_pending` and
`join_tokens.node_id` and allows the role `combined`. Nodes that run Kafka get their name as
advertised host, so the listeners and recorded addresses of existing clusters stay the same; the
other nodes get the host name their agent reported. Nodes of clusters that were deleted earlier
lose their role. Operations planned before the upgrade address their nodes by name and still
run.
