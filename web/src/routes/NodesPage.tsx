// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import {
  ActionIcon,
  Anchor,
  Badge,
  Button,
  Checkbox,
  EmptyState,
  Group,
  Modal,
  NumberInput,
  Paper,
  Select,
  Stack,
  Table,
  Text,
  TextInput,
  Tooltip,
} from '@mantine/core';
import { IconPencil, IconPlus, IconServerBolt, IconTrash } from '@tabler/icons-react';
import { useState } from 'react';
import { Link } from 'react-router';

import { useOverview } from '../api/clusters';
import { type NodeUpdate, useCreateJoinToken, useDeleteNode, useNodes, useUpdateNode } from '../api/nodes';
import type { AgentNode, JoinTokenResponse } from '../api/types';
import { confirmAction } from '../components/confirm';
import { CommandBlock, JoinCommand } from '../components/nodes/JoinCommand';
import { notifySuccess } from '../components/notify';
import { PageHeader } from '../components/PageHeader';
import { PageLoader, QueryError } from '../components/QueryState';
import { RelativeTime } from '../components/RelativeTime';
import { usePermissions } from '../hooks/usePermissions';
import { hostAddressError } from '../lib/bootstrap';
import { formatBytes, formatRelativeTime } from '../lib/format';
import { NODE_ROLE_COLORS, NODE_ROLE_LABELS } from '../lib/labels';
import { removalBlocker, type UninstallOptions, uninstallCommand } from '../lib/nodes';

const NODE_NAME = /^[A-Za-z0-9._-]{1,63}$/;
const RACK = /^[A-Za-z0-9._-]{0,64}$/;

/** Expiry of the agent certificate; agents renew it after two thirds of its lifetime. */
function Certificate({ node }: { node: AgentNode }) {
  if (node.transport === 'insecure') {
    return (
      <Tooltip
        label="Insecure transport behind a TLS-terminating proxy: messages are signed instead of using a client certificate."
        withArrow
        multiline
        maw={280}
      >
        <Badge size="sm" variant="light" color="gray">
          signed messages
        </Badge>
      </Tooltip>
    );
  }
  const state = node.certificateState;
  switch (state) {
    case '':
      return (
        <Text size="sm" c="dimmed">
          —
        </Text>
      );
    case 'ok':
      return <RelativeTime value={node.certNotAfter} />;
    case 'expiring':
      return (
        <Tooltip
          label="Not renewed in time: check that the agent is connected and runs version 0.2.0 or later."
          withArrow
          multiline
          maw={280}
        >
          <Badge size="sm" variant="light" color="yellow">
            expires {formatRelativeTime(node.certNotAfter)}
          </Badge>
        </Tooltip>
      );
    case 'expired':
      return (
        <Tooltip label="Enroll the node again with a new join token." withArrow>
          <Badge size="sm" variant="light" color="red">
            expired
          </Badge>
        </Tooltip>
      );
    default: {
      const unhandled: never = state;
      return unhandled;
    }
  }
}

function AddNodeModal({ opened, onClose }: { opened: boolean; onClose: () => void }) {
  const { data: clusters } = useOverview();
  const create = useCreateJoinToken();
  const [nodeName, setNodeName] = useState('');
  const [clusterId, setClusterId] = useState<string | null>(null);
  const [ttl, setTtl] = useState<number>(60);
  const [token, setToken] = useState<JoinTokenResponse | null>(null);

  const name = nodeName.trim();
  const nameError = name !== '' && !NODE_NAME.test(name) ? 'Letters, digits, ".", "_" and "-"' : undefined;

  const submit = () => {
    if (nameError) return;
    create.mutate(
      { nodeName: name || undefined, clusterId: clusterId ?? undefined, ttlMinutes: ttl },
      { onSuccess: setToken },
    );
  };

  return (
    <Modal opened={opened} onClose={onClose} title={<Text fw={600}>Add node</Text>} size="lg" closeOnClickOutside={!token}>
      {token ? (
        <Stack>
          <Text size="sm">
            Run this command on the new host. It installs brokerium-agent and joins it with a one-time token.
          </Text>
          <JoinCommand token={token} />
          <Group justify="flex-end">
            <Button onClick={onClose}>Done</Button>
          </Group>
        </Stack>
      ) : (
        <form
          onSubmit={(e) => {
            e.preventDefault();
            submit();
          }}
        >
          <Stack>
            <TextInput
              label="Node name"
              description="Optional: defaults to the host name the agent reports"
              placeholder="The host name"
              value={nodeName}
              onChange={(e) => setNodeName(e.currentTarget.value)}
              error={nameError}
              data-autofocus
            />
            <Select
              label="Cluster"
              description="Optional: attach the node to a cluster right away"
              placeholder="No cluster"
              data={(clusters ?? []).map((c) => ({ value: c.cluster.id, label: c.cluster.name }))}
              value={clusterId}
              onChange={setClusterId}
              clearable
            />
            <NumberInput
              label="Token lifetime (minutes)"
              min={5}
              max={7 * 24 * 60}
              value={ttl}
              onChange={(v) => setTtl(Number(v) || 60)}
            />
            <Group justify="flex-end">
              <Button variant="default" onClick={onClose}>
                Cancel
              </Button>
              <Button type="submit" loading={create.isPending} disabled={Boolean(nameError)}>
                Create join token
              </Button>
            </Group>
          </Stack>
        </form>
      )}
    </Modal>
  );
}

function EditNodeModal({ node, onClose }: { node: AgentNode; onClose: () => void }) {
  const update = useUpdateNode();
  const [name, setName] = useState(node.name);
  const [address, setAddress] = useState(node.advertisedHost);
  const [rack, setRack] = useState(node.rack);
  const nameError = NODE_NAME.test(name.trim()) ? undefined : 'Letters, digits, ".", "_" and "-", at most 63';
  const addressError = address.trim() === '' ? undefined : (hostAddressError(address) ?? undefined);
  const rackError = RACK.test(rack.trim()) ? undefined : 'Letters, digits, ".", "_" and "-", at most 64';
  const runsKafka = node.role !== '';
  const container = node.providerRef !== '';

  const submit = () => {
    if (nameError || addressError || rackError) return;
    const changes: NodeUpdate = {};
    if (name.trim() !== node.name) changes.name = name.trim();
    if (address.trim() !== node.advertisedHost) changes.advertisedHost = address.trim();
    if (rack.trim() !== node.rack) changes.rack = rack.trim();
    if (Object.keys(changes).length === 0) {
      onClose();
      return;
    }
    update.mutate(
      { id: node.id, update: changes },
      {
        onSuccess: (saved) => {
          notifySuccess('Node updated', saved.name);
          onClose();
        },
      },
    );
  };

  return (
    <Modal opened onClose={onClose} title={<Text fw={600}>Edit node {node.name}</Text>} size="lg">
      <form
        onSubmit={(e) => {
          e.preventDefault();
          submit();
        }}
      >
        <Stack>
          <TextInput
            label="Name"
            description={container ? 'Nodes of Docker clusters keep the name of their container' : undefined}
            required
            disabled={container}
            value={name}
            onChange={(e) => setName(e.currentTarget.value)}
            error={nameError}
          />
          <TextInput
            label="Advertised host"
            description={
              container
                ? 'Nodes of Docker clusters are reached through their container'
                : runsKafka
                  ? 'DNS name or IP address the Kafka listeners advertise. A change takes effect when the node is configured again, for example by a rolling restart.'
                  : 'DNS name or IP address the Kafka listeners will advertise. Leave empty for the host name the agent reports.'
            }
            disabled={container}
            placeholder={node.hostname}
            value={address}
            onChange={(e) => setAddress(e.currentTarget.value)}
            error={addressError}
            spellCheck={false}
          />
          <TextInput
            label="Rack"
            description="broker.rack of the node when it joins a cluster"
            placeholder="none"
            value={rack}
            onChange={(e) => setRack(e.currentTarget.value)}
            error={rackError}
          />
          <Group justify="flex-end">
            <Button variant="default" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" loading={update.isPending} disabled={Boolean(nameError || addressError || rackError)}>
              Save
            </Button>
          </Group>
        </Stack>
      </form>
    </Modal>
  );
}

/** The command that removes the agent of a removed node from its host, with the options of the script. */
function UninstallModal({ name, command, onClose }: { name: string; command: string; onClose: () => void }) {
  const [options, setOptions] = useState<UninstallOptions>({ stopKafka: false, removeKafka: false, deleteData: false });
  const option = (key: keyof UninstallOptions, label: string, description: string, color?: string) => (
    <Checkbox
      label={label}
      description={description}
      color={color}
      checked={options[key]}
      onChange={(e) => {
        const checked = e.currentTarget.checked;
        setOptions((o) => ({ ...o, [key]: checked }));
      }}
    />
  );

  return (
    <Modal opened onClose={onClose} title={<Text fw={600}>Remove the agent from {name}</Text>} size="lg">
      <Stack>
        <Text size="sm">
          The agent stops as soon as it is connected, and systemd does not start it again. Run this command on the host
          as a user with sudo to remove the agent, its service, sudo rule, configuration and state.
        </Text>
        <CommandBlock command={uninstallCommand(command, options)} />
        {option(
          'stopKafka',
          'Stop Kafka (--stop-kafka)',
          'Kafka runs from the configuration of the agent: without this option the script changes nothing while Kafka runs.',
        )}
        {option('removeKafka', 'Remove the Kafka releases (--remove-kafka)', 'The releases the agent installed in /opt/brokerium/kafka.')}
        {option(
          'deleteData',
          'Delete the Kafka data (--delete-data)',
          'The data directory of the agent and all data in it are lost.',
          'red',
        )}
        <Text size="xs" c="dimmed">
          Running the script again removes what is left.
        </Text>
        <Group justify="flex-end">
          <Button onClick={onClose}>Done</Button>
        </Group>
      </Stack>
    </Modal>
  );
}

export default function NodesPage() {
  const nodes = useNodes();
  const { data: clusters } = useOverview();
  const { canOperate } = usePermissions();
  const remove = useDeleteNode();
  const [adding, setAdding] = useState(false);
  const [editing, setEditing] = useState<AgentNode | null>(null);
  const [uninstall, setUninstall] = useState<{ name: string; command: string } | null>(null);
  const clusterName = (id: string | null) => (id ? (clusters?.find((c) => c.cluster.id === id)?.cluster.name ?? id) : null);

  const del = async (node: AgentNode) => {
    const ok = await confirmAction({
      title: `Remove node ${node.name}`,
      message:
        node.role === 'combined'
          ? 'Brokerium removes a combined node only after it left its cluster: take it out of the quorum, stop Kafka and unregister its broker first. Its agent then stops for good, and the next dialog shows the command that removes it from the host.'
          : 'The agent stops for good and its registration and credentials are revoked; Kafka on the host is left as it is. The next dialog shows the command that removes the agent, and Kafka and its data if you want, from the host.',
      confirmLabel: 'Remove node',
      typeToConfirm: node.name,
      typeToConfirmWhat: 'node name',
    });
    if (ok) {
      remove.mutate(node.id, {
        onSuccess: (res) => {
          notifySuccess('Node removed', node.name);
          setUninstall({ name: node.name, command: res.uninstallCommand });
        },
      });
    }
  };

  const list = [...(nodes.data ?? [])].sort((a, b) => a.name.localeCompare(b.name));

  return (
    <>
      <PageHeader
        title="Nodes"
        description="Hosts running brokerium-agent"
        actions={
          canOperate && (
            <Button leftSection={<IconPlus size={16} />} onClick={() => setAdding(true)}>
              Add node
            </Button>
          )
        }
      />
      {adding && <AddNodeModal opened onClose={() => setAdding(false)} />}
      {editing && <EditNodeModal node={editing} onClose={() => setEditing(null)} />}
      {uninstall && <UninstallModal name={uninstall.name} command={uninstall.command} onClose={() => setUninstall(null)} />}

      {nodes.isPending && <PageLoader />}
      {nodes.error && !nodes.data && <QueryError error={nodes.error} onRetry={() => void nodes.refetch()} />}
      {nodes.data && (
        <Paper withBorder radius="md">
          {list.length === 0 ? (
            <EmptyState
              py="xl"
              icon={<IconServerBolt size={28} />}
              title="No agent nodes"
              description="Add a node to install brokerium-agent on a Kafka host with a one-time join token."
            />
          ) : (
            <Table.ScrollContainer minWidth={1300}>
              <Table highlightOnHover verticalSpacing="sm">
                <Table.Thead>
                  <Table.Tr>
                    <Table.Th>Name</Table.Th>
                    <Table.Th>Cluster</Table.Th>
                    <Table.Th>Role</Table.Th>
                    <Table.Th>Kafka node id</Table.Th>
                    <Table.Th>Address</Table.Th>
                    <Table.Th>Rack</Table.Th>
                    <Table.Th>Agent</Table.Th>
                    <Table.Th>Connection</Table.Th>
                    <Table.Th>Last seen</Table.Th>
                    <Table.Th>Certificate</Table.Th>
                    <Table.Th>Kafka</Table.Th>
                    <Table.Th w={72} />
                  </Table.Tr>
                </Table.Thead>
                <Table.Tbody>
                  {list.map((n) => {
                    const cluster = clusterName(n.clusterId);
                    const blocker = removalBlocker(n, cluster ?? 'its cluster');
                    return (
                      <Table.Tr key={n.id}>
                        <Table.Td>
                          <Text size="sm" fw={600}>
                            {n.name}
                          </Text>
                          {n.providerRef && (
                            <Text size="xs" c="dimmed" ff="monospace">
                              {n.providerRef}
                            </Text>
                          )}
                        </Table.Td>
                        <Table.Td>
                          {cluster && n.clusterId ? (
                            <Anchor component={Link} to={`/clusters/${encodeURIComponent(n.clusterId)}`} size="sm">
                              {cluster}
                            </Anchor>
                          ) : (
                            <Text size="sm" c="dimmed">
                              unassigned
                            </Text>
                          )}
                        </Table.Td>
                        <Table.Td>
                          {n.role ? (
                            <Badge size="sm" variant="light" color={NODE_ROLE_COLORS[n.role]}>
                              {NODE_ROLE_LABELS[n.role]}
                            </Badge>
                          ) : (
                            <Text size="sm" c="dimmed">
                              —
                            </Text>
                          )}
                        </Table.Td>
                        <Table.Td>{n.kafkaNodeId ?? '—'}</Table.Td>
                        <Table.Td>
                          <Text size="sm" ff="monospace">
                            {n.advertisedHost || n.hostname || n.status.hostname || '—'}
                          </Text>
                          {n.hostname && n.advertisedHost && n.hostname !== n.advertisedHost && (
                            <Text size="xs" c="dimmed" ff="monospace">
                              host {n.hostname}
                            </Text>
                          )}
                        </Table.Td>
                        <Table.Td>{n.rack || <Text c="dimmed">—</Text>}</Table.Td>
                        <Table.Td>
                          <Text size="sm" ff="monospace">
                            {n.agentVersion || '—'}
                          </Text>
                        </Table.Td>
                        <Table.Td>
                          <Badge size="sm" variant="dot" color={n.connected ? 'green' : 'red'}>
                            {n.connected ? 'connected' : 'disconnected'}
                          </Badge>
                        </Table.Td>
                        <Table.Td>
                          <RelativeTime value={n.lastSeenAt} fallback="never" />
                        </Table.Td>
                        <Table.Td>
                          <Certificate node={n} />
                        </Table.Td>
                        <Table.Td>
                          <Tooltip
                            label={
                              <Stack gap={0}>
                                {n.status.processRoles && <Text size="xs">process.roles: {n.status.processRoles}</Text>}
                                {n.status.dataBytes !== undefined && <Text size="xs">data: {formatBytes(n.status.dataBytes)}</Text>}
                                {n.status.diskFreeBytes !== undefined && (
                                  <Text size="xs">disk free: {formatBytes(n.status.diskFreeBytes)}</Text>
                                )}
                                {n.status.lastExit && <Text size="xs">last exit: {n.status.lastExit}</Text>}
                                {n.status.pid ? <Text size="xs">pid: {n.status.pid}</Text> : null}
                              </Stack>
                            }
                            withArrow
                            multiline
                          >
                            <Group gap={6} wrap="nowrap">
                              <Badge size="sm" variant="light" color={n.status.running ? 'green' : 'gray'}>
                                {n.status.running ? 'running' : 'stopped'}
                              </Badge>
                              <Text size="sm" ff="monospace">
                                {n.status.activeVersion || '—'}
                              </Text>
                            </Group>
                          </Tooltip>
                        </Table.Td>
                        <Table.Td>
                          {canOperate && (
                            <Group gap={4} wrap="nowrap">
                              <Tooltip label="Edit node" withArrow>
                                <ActionIcon variant="subtle" color="gray" onClick={() => setEditing(n)} aria-label={`Edit ${n.name}`}>
                                  <IconPencil size={14} />
                                </ActionIcon>
                              </Tooltip>
                              <Tooltip label={blocker ?? 'Remove node'} withArrow multiline maw={blocker ? 320 : undefined}>
                                <ActionIcon
                                  variant="subtle"
                                  color="red"
                                  data-disabled={blocker !== null || undefined}
                                  aria-disabled={blocker !== null}
                                  onClick={() => {
                                    if (blocker === null) void del(n);
                                  }}
                                  aria-label={`Remove ${n.name}`}
                                >
                                  <IconTrash size={14} />
                                </ActionIcon>
                              </Tooltip>
                            </Group>
                          )}
                        </Table.Td>
                      </Table.Tr>
                    );
                  })}
                </Table.Tbody>
              </Table>
            </Table.ScrollContainer>
          )}
        </Paper>
      )}
    </>
  );
}
