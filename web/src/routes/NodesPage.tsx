// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import {
  ActionIcon,
  Alert,
  Anchor,
  Badge,
  Button,
  Code,
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
import { IconInfoCircle, IconPlus, IconServerBolt, IconTrash } from '@tabler/icons-react';
import { useState } from 'react';
import { Link } from 'react-router';

import { useOverview } from '../api/clusters';
import { useCreateJoinToken, useDeleteNode, useNodes } from '../api/nodes';
import type { AgentNode, JoinTokenResponse } from '../api/types';
import { confirmAction } from '../components/confirm';
import { CopyIcon } from '../components/CopyText';
import { notifySuccess } from '../components/notify';
import { PageHeader } from '../components/PageHeader';
import { PageLoader, QueryError } from '../components/QueryState';
import { RelativeTime } from '../components/RelativeTime';
import { usePermissions } from '../hooks/usePermissions';
import { formatBytes, formatDateTime, formatRelativeTime } from '../lib/format';

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

  const submit = () => {
    if (!nodeName.trim()) return;
    create.mutate(
      { nodeName: nodeName.trim(), clusterId: clusterId ?? undefined, ttlMinutes: ttl },
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
          <Paper withBorder p="sm" radius="md" pos="relative">
            <Code block style={{ whiteSpace: 'pre-wrap', wordBreak: 'break-all', paddingRight: 36 }}>
              {token.command}
            </Code>
            <Group pos="absolute" top={8} right={8}>
              <CopyIcon value={token.command} label="Copy command" />
            </Group>
          </Paper>
          <Alert color="yellow" variant="light" icon={<IconInfoCircle size={18} />}>
            The token is shown only once and expires {formatDateTime(token.expiresAt)} (
            <RelativeTime value={token.expiresAt} size="sm" />
            ).
          </Alert>
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
              placeholder="kafka-broker-4"
              required
              value={nodeName}
              onChange={(e) => setNodeName(e.currentTarget.value)}
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
              <Button type="submit" loading={create.isPending} disabled={!nodeName.trim()}>
                Create join token
              </Button>
            </Group>
          </Stack>
        </form>
      )}
    </Modal>
  );
}

export default function NodesPage() {
  const nodes = useNodes();
  const { data: clusters } = useOverview();
  const { canOperate } = usePermissions();
  const remove = useDeleteNode();
  const [adding, setAdding] = useState(false);
  const clusterName = (id: string | null) => (id ? (clusters?.find((c) => c.cluster.id === id)?.cluster.name ?? id) : null);

  const del = async (id: string, name: string) => {
    const ok = await confirmAction({
      title: `Remove node ${name}`,
      message: 'The agent registration and its certificate are revoked. Kafka on the host keeps running but is no longer managed.',
      confirmLabel: 'Remove node',
      typeToConfirm: name,
      typeToConfirmWhat: 'node name',
    });
    if (ok) remove.mutate(id, { onSuccess: () => notifySuccess('Node removed', name) });
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
            <Table.ScrollContainer minWidth={1220}>
              <Table highlightOnHover verticalSpacing="sm">
                <Table.Thead>
                  <Table.Tr>
                    <Table.Th>Name</Table.Th>
                    <Table.Th>Cluster</Table.Th>
                    <Table.Th>Role</Table.Th>
                    <Table.Th>Kafka node id</Table.Th>
                    <Table.Th>Hostname</Table.Th>
                    <Table.Th>Agent</Table.Th>
                    <Table.Th>Connection</Table.Th>
                    <Table.Th>Last seen</Table.Th>
                    <Table.Th>Certificate</Table.Th>
                    <Table.Th>Kafka</Table.Th>
                    <Table.Th w={48} />
                  </Table.Tr>
                </Table.Thead>
                <Table.Tbody>
                  {list.map((n) => {
                    const cluster = clusterName(n.clusterId);
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
                            <Badge size="sm" variant="light" color={n.role === 'controller' ? 'grape' : 'indigo'}>
                              {n.role}
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
                            {n.hostname || n.status.hostname || '—'}
                          </Text>
                        </Table.Td>
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
                            <Tooltip label="Remove node" withArrow>
                              <ActionIcon variant="subtle" color="red" onClick={() => void del(n.id, n.name)} aria-label={`Remove ${n.name}`}>
                                <IconTrash size={14} />
                              </ActionIcon>
                            </Tooltip>
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
