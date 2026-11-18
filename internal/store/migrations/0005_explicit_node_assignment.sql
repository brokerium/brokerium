-- SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

-- Operators choose the hosts of agent clusters instead of naming them by convention (ADR 0018).
-- advertised_host is the address the listeners of a node advertise and the quorum and the server
-- use; rack is the broker.rack a node gets when it joins a cluster. combined runs broker and
-- controller in one process. kafka_stop_pending marks a host that was released from its cluster
-- while its agent was not connected: Kafka is stopped on it when the agent connects again.
ALTER TABLE nodes
    ADD COLUMN advertised_host    TEXT NOT NULL DEFAULT '',
    ADD COLUMN rack               TEXT NOT NULL DEFAULT '',
    ADD COLUMN kafka_stop_pending BOOLEAN NOT NULL DEFAULT false,
    ADD CONSTRAINT nodes_role_check CHECK (role IN ('', 'controller', 'broker', 'combined'));

-- Nodes of deleted clusters kept their role; deleting a cluster now releases its nodes.
UPDATE nodes SET role = '', kafka_node_id = NULL WHERE cluster_id IS NULL AND role <> '';

-- Nodes that run Kafka keep advertising their name, so the listeners of existing clusters stay
-- the same; the others start with the host name their agent reported.
UPDATE nodes SET advertised_host = CASE WHEN role <> '' THEN name ELSE hostname END;

-- A join token names its node, leaves the name to the host name the agent reports at enrollment
-- (empty node_name) or enrolls an existing node again (node_id).
ALTER TABLE join_tokens
    ALTER COLUMN node_name SET DEFAULT '',
    ADD COLUMN node_id TEXT REFERENCES nodes (id) ON DELETE CASCADE;
