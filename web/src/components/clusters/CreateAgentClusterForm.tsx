// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import {
  Alert,
  Badge,
  Button,
  EmptyState,
  Group,
  List,
  NumberInput,
  Paper,
  Select,
  Stack,
  Table,
  Text,
  TextInput,
  Tooltip,
} from '@mantine/core';
import { IconAlertTriangle, IconServerBolt } from '@tabler/icons-react';
import { useState } from 'react';

import { type PlannedCluster, useCreateNewCluster } from '../../api/clusters';
import { useNodes, useUpdateNode } from '../../api/nodes';
import {
  type HostPlan,
  type HostRole,
  assignNodeIds,
  bootstrapNodes,
  checkBootstrap,
  hostAddressError,
  hostData,
  initialHostPlan,
  isBroker,
  isFreeHost,
  isVoter,
} from '../../lib/bootstrap';
import { CLUSTER_NAME_PATTERN } from '../../lib/clusterForm';
import { KAFKA_20979_WARNING, affectedByKafka20979, parseKafkaVersion } from '../../lib/kafkaVersion';
import { NODE_ROLE_LABELS } from '../../lib/labels';
import { quorumHint } from '../../lib/quorum';
import { AddHost } from '../nodes/JoinCommand';
import { PlannedClusterPreview, type Quorum, QuorumField } from './NewClusterParts';

const ROLE_OPTIONS: { value: HostRole; label: string }[] = [
  { value: 'unused', label: 'Not used' },
  { value: 'controller', label: NODE_ROLE_LABELS.controller },
  { value: 'broker', label: NODE_ROLE_LABELS.broker },
  { value: 'combined', label: NODE_ROLE_LABELS.combined },
];

type HostEdit = Partial<Omit<HostPlan, 'node'>>;

interface CreateAgentClusterFormProps {
  onCancel: () => void;
  onDone: () => void;
}

/**
 * Creates a cluster on hosts that run brokerium-agent: the operator gives every chosen host a role,
 * a node id, a rack and the address its listeners advertise, then starts the bootstrap-cluster plan.
 */
export function CreateAgentClusterForm({ onCancel, onDone }: CreateAgentClusterFormProps) {
  const create = useCreateNewCluster();
  const update = useUpdateNode();
  const [name, setName] = useState('');
  const [version, setVersion] = useState('4.4.0');
  const [quorum, setQuorum] = useState<Quorum>('dynamic');
  const [edits, setEdits] = useState<Record<string, HostEdit>>({});
  const [waiting, setWaiting] = useState(false);
  const [saving, setSaving] = useState(false);
  const [planned, setPlanned] = useState<PlannedCluster | null>(null);
  const nodes = useNodes(undefined, waiting ? 3_000 : 10_000);

  const hosts = (nodes.data ?? [])
    .filter((n) => isFreeHost(n))
    .sort((a, b) => a.name.localeCompare(b.name))
    .map((n): HostPlan => ({ ...initialHostPlan(n), ...edits[n.id] }));
  const ids = assignNodeIds(hosts);
  const check = checkBootstrap(hosts);
  const chosen = hosts.filter((h) => h.role !== 'unused');
  const voters = chosen.filter((h) => isVoter(h.role)).length;
  const brokers = chosen.filter((h) => isBroker(h.role)).length;
  const nameError = name !== '' && !CLUSTER_NAME_PATTERN.test(name) ? 'Lowercase letters, digits and dashes' : undefined;
  const versionError = parseKafkaVersion(version) ? undefined : 'A release such as 4.3.1';
  const valid = name !== '' && !nameError && !versionError && check.errors.length === 0;

  const edit = (id: string, change: HostEdit) => setEdits((all) => ({ ...all, [id]: { ...all[id], ...change } }));

  const submit = async () => {
    if (!valid) return;
    setSaving(true);
    try {
      for (const h of chosen) {
        const address = h.address.trim();
        if (address !== h.node.advertisedHost) await update.mutateAsync({ id: h.node.id, update: { advertisedHost: address } });
      }
    } catch {
      // The mutation cache already notified the user.
      return;
    } finally {
      setSaving(false);
    }
    create.mutate(
      { name, provider: 'agent', params: { version: version.trim(), quorum, nodes: bootstrapNodes(hosts) } },
      { onSuccess: setPlanned },
    );
  };

  if (planned) return <PlannedClusterPreview planned={planned} onBack={() => setPlanned(null)} onDone={onDone} />;

  return (
    <Stack>
      <Text size="sm" c="dimmed">
        Brokerium installs Kafka on the chosen hosts through their agents, formats the storage and starts the controllers
        before the brokers. Hosts keep their names; Kafka listeners advertise the address in the table, which clients and
        the other nodes must be able to resolve.
      </Text>
      <TextInput
        label="Name"
        required
        value={name}
        onChange={(e) => setName(e.currentTarget.value)}
        error={nameError}
        data-autofocus
      />
      <Group grow align="flex-start">
        <TextInput label="Kafka version" value={version} onChange={(e) => setVersion(e.currentTarget.value)} error={versionError} />
        <QuorumField value={quorum} onChange={setQuorum} />
      </Group>

      <Stack gap={4}>
        <Text size="sm" fw={500}>
          Hosts
        </Text>
        <Text size="xs" c="dimmed">
          Connected agents that belong to no cluster. Leave the node id empty to number controllers from 1 and brokers
          from 101.
        </Text>
      </Stack>
      <Paper withBorder radius="md">
        {hosts.length === 0 ? (
          <EmptyState
            py="lg"
            icon={<IconServerBolt size={24} />}
            title={nodes.isPending ? 'Loading hosts…' : 'No free hosts'}
            description="Hosts with brokerium-agent that belong to no cluster appear here. Add a host to install the agent with a one-time join token."
          />
        ) : (
          <Table.ScrollContainer minWidth={880}>
            <Table verticalSpacing="xs">
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>Host</Table.Th>
                  <Table.Th w={190}>Role</Table.Th>
                  <Table.Th w={120}>Node id</Table.Th>
                  <Table.Th w={130}>Rack</Table.Th>
                  <Table.Th w={240}>Address</Table.Th>
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {hosts.map((h, i) => {
                  const used = h.role !== 'unused';
                  const auto = ids[i];
                  const data = hostData(h.node);
                  return (
                    <Table.Tr key={h.node.id}>
                      <Table.Td>
                        <Text size="sm" fw={600}>
                          {h.node.name}
                        </Text>
                        <Group gap={6} wrap="nowrap">
                          <Badge size="xs" variant="dot" color={h.node.connected ? 'green' : 'red'}>
                            {h.node.connected ? 'connected' : 'disconnected'}
                          </Badge>
                          {data && (
                            <Tooltip label={`Keeps ${data}. Clear its data directory to use the host.`} withArrow multiline maw={280}>
                              <Badge size="xs" variant="light" color="yellow">
                                Kafka data
                              </Badge>
                            </Tooltip>
                          )}
                          {h.node.hostname && h.node.hostname !== h.node.name && (
                            <Text size="xs" c="dimmed" ff="monospace">
                              {h.node.hostname}
                            </Text>
                          )}
                        </Group>
                      </Table.Td>
                      <Table.Td>
                        <Select
                          size="xs"
                          aria-label={`Role of ${h.node.name}`}
                          data={ROLE_OPTIONS}
                          value={h.role}
                          onChange={(v) => v && edit(h.node.id, { role: v as HostRole })}
                          allowDeselect={false}
                          disabled={!h.node.connected || (data !== null && !used)}
                        />
                      </Table.Td>
                      <Table.Td>
                        <NumberInput
                          size="xs"
                          aria-label={`Node id of ${h.node.name}`}
                          min={0}
                          allowDecimal={false}
                          allowNegative={false}
                          placeholder={used && auto !== null ? `auto (${auto})` : 'auto'}
                          value={h.nodeId}
                          onChange={(v) => edit(h.node.id, { nodeId: v === '' ? '' : Number(v) })}
                          disabled={!used}
                        />
                      </Table.Td>
                      <Table.Td>
                        <TextInput
                          size="xs"
                          aria-label={`Rack of ${h.node.name}`}
                          placeholder="none"
                          value={h.rack}
                          onChange={(e) => edit(h.node.id, { rack: e.currentTarget.value })}
                          disabled={!used}
                        />
                      </Table.Td>
                      <Table.Td>
                        <TextInput
                          size="xs"
                          aria-label={`Address of ${h.node.name}`}
                          value={h.address}
                          onChange={(e) => edit(h.node.id, { address: e.currentTarget.value })}
                          error={used ? hostAddressError(h.address) : null}
                          disabled={!used}
                          spellCheck={false}
                        />
                      </Table.Td>
                    </Table.Tr>
                  );
                })}
              </Table.Tbody>
            </Table>
          </Table.ScrollContainer>
        )}
      </Paper>
      <AddHost onWaitingChange={setWaiting} />

      {chosen.length > 0 && (
        <Text size="xs" c="dimmed">
          {voters} {voters === 1 ? 'controller' : 'controllers'} and {brokers} {brokers === 1 ? 'broker' : 'brokers'}
          {voters % 2 === 1 ? `. ${quorumHint(voters)}` : '.'}
        </Text>
      )}
      {chosen.length > 0 && check.errors.length > 0 && (
        <Alert color="red" variant="light" icon={<IconAlertTriangle size={18} />}>
          <List size="sm">
            {check.errors.map((e) => (
              <List.Item key={e}>{e}</List.Item>
            ))}
          </List>
        </Alert>
      )}
      {check.warnings.map((w) => (
        <Alert key={w} color="yellow" variant="light" icon={<IconAlertTriangle size={18} />}>
          {w}
        </Alert>
      ))}
      {affectedByKafka20979(version) && (
        <Alert color="yellow" variant="light" icon={<IconAlertTriangle size={18} />}>
          {KAFKA_20979_WARNING}
        </Alert>
      )}
      <Group justify="flex-end">
        <Button variant="default" onClick={onCancel}>
          Cancel
        </Button>
        <Button disabled={!valid} loading={saving || create.isPending} onClick={() => void submit()}>
          Preview plan
        </Button>
      </Group>
    </Stack>
  );
}
