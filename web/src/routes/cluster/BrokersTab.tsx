// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import {
  ActionIcon,
  Checkbox,
  Group,
  Menu,
  MultiSelect,
  NumberInput,
  Paper,
  Progress,
  SegmentedControl,
  Stack,
  Table,
  Text,
  TextInput,
  Tooltip,
} from '@mantine/core';
import {
  IconAlertTriangle,
  IconArrowsExchange,
  IconBan,
  IconDotsVertical,
  IconHandStop,
  IconPlus,
  IconReplace,
  IconRotateClockwise,
  IconSettings,
  IconTrash,
} from '@tabler/icons-react';
import { useState } from 'react';

import type { Broker } from '../../api/types';
import { BrokerStateBadges } from '../../components/badges';
import { GatedButton, GatedMenuItem } from '../../components/Gated';
import { usePlanner } from '../../components/operations/plannerContext';
import { diskUsedPercent } from '../../lib/distribution';
import { formatBytes, formatNumber, formatPercent } from '../../lib/format';
import { diskUsageColor } from '../../lib/status';
import { BrokerConfigsDrawer } from './BrokerConfigsDrawer';
import { useClusterContext } from './clusterContext';
import { NoSnapshot, ParamsModal } from './common';
import { NodePoolsSection } from './NodePoolsSection';

type BrokerDialog =
  | { kind: 'add' }
  | { kind: 'restart' }
  | { kind: 'drain'; broker: Broker; decommission: boolean }
  | { kind: 'replace'; broker: Broker };

const MiB = 1024 * 1024;

function DiskUsage({ broker }: { broker: Broker }) {
  const percent = diskUsedPercent(broker);
  const errors = (broker.logDirs ?? []).filter((d) => d.error);
  if (percent === null) {
    return (
      <Text size="sm" c="dimmed">
        —
      </Text>
    );
  }
  const used = broker.diskTotalBytes - broker.diskUsableBytes;
  return (
    <Tooltip
      label={
        <Stack gap={2}>
          <Text size="xs">
            {formatBytes(used)} used of {formatBytes(broker.diskTotalBytes)}
          </Text>
          {(broker.logDirs ?? []).map((d) => (
            <Text size="xs" key={d.path}>
              {d.path}: {formatBytes(d.dataBytes)} data{d.cordoned ? ', cordoned' : ''}
              {d.error ? `, error: ${d.error}` : ''}
            </Text>
          ))}
        </Stack>
      }
      withArrow
      multiline
    >
      <Group gap={6} wrap="nowrap" w={150}>
        <Progress value={percent} color={diskUsageColor(percent)} size="md" style={{ flex: 1 }} />
        <Text size="xs" w={36} ta="right">
          {formatPercent(percent)}
        </Text>
        {errors.length > 0 && <IconAlertTriangle size={14} color="var(--mantine-color-red-6)" />}
      </Group>
    </Tooltip>
  );
}

export default function BrokersTab() {
  const { snapshot, cluster } = useClusterContext();
  const planner = usePlanner();
  const [configBroker, setConfigBroker] = useState<number | null>(null);
  const [dialog, setDialog] = useState<BrokerDialog | null>(null);
  const [addCount, setAddCount] = useState<number>(1);
  const [addRack, setAddRack] = useState('');
  const [addRebalance, setAddRebalance] = useState(true);
  const [restartNodes, setRestartNodes] = useState<string[]>([]);
  const [throttle, setThrottle] = useState<number | ''>('');
  const [replaceMode, setReplaceMode] = useState<'new-id' | 'same-id'>('new-id');

  if (!snapshot) return <NoSnapshot />;

  const brokers = [...snapshot.brokers].sort((a, b) => a.id - b.id);
  const nodeIds = [
    ...new Set([...brokers.map((b) => b.id), ...(snapshot.quorum?.voters.map((v) => v.nodeId) ?? [])]),
  ].sort((a, b) => a - b);
  const totals = brokers.reduce(
    (acc, b) => ({ replicas: acc.replicas + b.replicas, leaders: acc.leaders + b.leaders, data: acc.data + b.dataBytes }),
    { replicas: 0, leaders: 0, data: 0 },
  );
  const close = () => setDialog(null);
  const throttleBytes = throttle === '' ? undefined : Math.round(throttle * MiB);

  const openDrain = (broker: Broker, decommission: boolean) => {
    setThrottle('');
    setDialog({ kind: 'drain', broker, decommission });
  };

  return (
    <Stack gap="md">
      <Group justify="space-between">
        <Text size="sm" c="dimmed">
          {brokers.length} brokers · {formatNumber(totals.replicas)} replicas · {formatNumber(totals.leaders)} leaders ·{' '}
          {formatBytes(totals.data)} data
        </Text>
        <Group gap="xs">
          <GatedButton
            availability={planner.availability('add-broker')}
            leftSection={<IconPlus size={16} />}
            onClick={() => setDialog({ kind: 'add' })}
          >
            Add broker
          </GatedButton>
          <GatedButton
            availability={planner.availability('rolling-restart')}
            variant="default"
            leftSection={<IconRotateClockwise size={16} />}
            onClick={() => {
              setRestartNodes([]);
              setDialog({ kind: 'restart' });
            }}
          >
            Rolling restart
          </GatedButton>
        </Group>
      </Group>

      <Paper withBorder radius="md">
        <Table.ScrollContainer minWidth={1100}>
          <Table highlightOnHover verticalSpacing="sm">
            <Table.Thead>
              <Table.Tr>
                <Table.Th>ID</Table.Th>
                <Table.Th>Host</Table.Th>
                <Table.Th>Rack</Table.Th>
                <Table.Th>Status</Table.Th>
                <Table.Th ta="right">Replicas</Table.Th>
                <Table.Th ta="right">Leaders</Table.Th>
                <Table.Th ta="right">Preferred leaders</Table.Th>
                <Table.Th ta="right">Data</Table.Th>
                <Table.Th>Disk usage</Table.Th>
                <Table.Th w={48} />
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {brokers.map((b) => {
                const unbalanced = b.leaders !== b.preferredLeaders;
                return (
                  <Table.Tr key={b.id}>
                    <Table.Td>
                      <Text fw={700}>{b.id}</Text>
                    </Table.Td>
                    <Table.Td>
                      <Text size="sm" ff="monospace">
                        {b.host}:{b.port}
                      </Text>
                    </Table.Td>
                    <Table.Td>{b.rack || <Text c="dimmed">—</Text>}</Table.Td>
                    <Table.Td>
                      <BrokerStateBadges broker={b} />
                    </Table.Td>
                    <Table.Td ta="right">{formatNumber(b.replicas)}</Table.Td>
                    <Table.Td ta="right">{formatNumber(b.leaders)}</Table.Td>
                    <Table.Td ta="right">
                      <Tooltip label="Leaders differ from preferred leaders" disabled={!unbalanced} withArrow>
                        <Text size="sm" c={unbalanced ? 'yellow.8' : undefined}>
                          {formatNumber(b.preferredLeaders)}
                        </Text>
                      </Tooltip>
                    </Table.Td>
                    <Table.Td ta="right">{formatBytes(b.dataBytes)}</Table.Td>
                    <Table.Td>
                      <DiskUsage broker={b} />
                    </Table.Td>
                    <Table.Td>
                      <Menu position="bottom-end" withArrow width={220}>
                        <Menu.Target>
                          <ActionIcon variant="subtle" color="gray" aria-label={`Actions for broker ${b.id}`}>
                            <IconDotsVertical size={16} />
                          </ActionIcon>
                        </Menu.Target>
                        <Menu.Dropdown>
                          <Menu.Item
                            leftSection={<IconSettings size={16} />}
                            onClick={() => setConfigBroker(b.id)}
                            disabled={!b.alive}
                            title={b.alive ? undefined : 'Configs can only be read from a live broker'}
                          >
                            Dynamic configs
                          </Menu.Item>
                          <Menu.Divider />
                          <Menu.Label>Operations</Menu.Label>
                          <GatedMenuItem
                            availability={planner.availability('cordon-broker')}
                            leftSection={<IconHandStop size={16} />}
                            onClick={() =>
                              void planner.plan('cordon-broker', { brokerId: b.id, cordon: !b.cordoned })
                            }
                          >
                            {b.cordoned ? 'Uncordon' : 'Cordon'}
                          </GatedMenuItem>
                          <GatedMenuItem
                            availability={planner.availability('drain-broker')}
                            leftSection={<IconArrowsExchange size={16} />}
                            onClick={() => openDrain(b, false)}
                          >
                            Drain
                          </GatedMenuItem>
                          <GatedMenuItem
                            availability={planner.availability('drain-broker')}
                            leftSection={<IconBan size={16} />}
                            onClick={() => openDrain(b, true)}
                          >
                            Decommission
                          </GatedMenuItem>
                          <GatedMenuItem
                            availability={planner.availability('replace-broker')}
                            leftSection={<IconReplace size={16} />}
                            onClick={() => {
                              setReplaceMode('new-id');
                              setDialog({ kind: 'replace', broker: b });
                            }}
                          >
                            Replace
                          </GatedMenuItem>
                          {(b.fenced || !b.alive) && (
                            <GatedMenuItem
                              availability={planner.availability('unregister-broker')}
                              color="red"
                              leftSection={<IconTrash size={16} />}
                              onClick={() => void planner.plan('unregister-broker', { brokerId: b.id })}
                            >
                              Unregister
                            </GatedMenuItem>
                          )}
                        </Menu.Dropdown>
                      </Menu>
                    </Table.Td>
                  </Table.Tr>
                );
              })}
            </Table.Tbody>
          </Table>
        </Table.ScrollContainer>
      </Paper>

      {cluster.provider === 'strimzi' && <NodePoolsSection />}

      <BrokerConfigsDrawer brokerId={configBroker} onClose={() => setConfigBroker(null)} />

      <ParamsModal
        opened={dialog?.kind === 'add'}
        onClose={close}
        title="Add broker"
        description="Provisions new broker nodes, waits until they join the cluster and optionally rebalances replicas onto them."
        planning={planner.pendingKey === 'add-broker'}
        onPlan={() =>
          planner.plan('add-broker', {
            count: addCount,
            rack: addRack.trim() || undefined,
            rebalance: addRebalance,
          })
        }
      >
        <NumberInput label="Number of brokers" min={1} max={10} value={addCount} onChange={(v) => setAddCount(Number(v) || 1)} />
        <TextInput label="Rack" placeholder="Optional" value={addRack} onChange={(e) => setAddRack(e.currentTarget.value)} />
        <Checkbox
          label="Rebalance replicas onto the new brokers"
          checked={addRebalance}
          onChange={(e) => setAddRebalance(e.currentTarget.checked)}
        />
      </ParamsModal>

      <ParamsModal
        opened={dialog?.kind === 'restart'}
        onClose={close}
        title="Rolling restart"
        description="Restarts nodes one at a time; each restart waits for the health gate (no offline or under-replicated partitions, quorum caught up)."
        planning={planner.pendingKey === 'rolling-restart'}
        onPlan={() =>
          planner.plan('rolling-restart', {
            nodeIds: restartNodes.length > 0 ? restartNodes.map(Number) : undefined,
          })
        }
      >
        <MultiSelect
          label="Nodes"
          description="Leave empty to restart every node"
          placeholder="All nodes"
          data={nodeIds.map(String)}
          value={restartNodes}
          onChange={setRestartNodes}
          clearable
          searchable
        />
      </ParamsModal>

      {dialog?.kind === 'drain' && (
        <ParamsModal
          opened
          onClose={close}
          title={`${dialog.decommission ? 'Decommission' : 'Drain'} broker ${dialog.broker.id}`}
          description={
            dialog.decommission
              ? `Cordons broker ${dialog.broker.id}, moves all its replicas and leadership to other brokers, then removes it from the cluster.`
              : `Cordons broker ${dialog.broker.id} and moves all its replicas and leadership to other brokers. The broker keeps running.`
          }
          planning={planner.pendingKey === 'drain-broker'}
          onPlan={() =>
            planner.plan('drain-broker', {
              brokerId: dialog.broker.id,
              decommission: dialog.decommission || undefined,
              throttleBytesPerSec: throttleBytes,
            })
          }
        >
          <NumberInput
            label="Replication throttle (MiB/s)"
            description="Leave empty to use the server default; the throttle is always removed at the end"
            min={1}
            value={throttle}
            onChange={(v) => setThrottle(v === '' ? '' : Number(v))}
          />
        </ParamsModal>
      )}

      {dialog?.kind === 'replace' && (
        <ParamsModal
          opened
          onClose={close}
          title={`Replace broker ${dialog.broker.id}`}
          planning={planner.pendingKey === 'replace-broker'}
          onPlan={() => planner.plan('replace-broker', { brokerId: dialog.broker.id, mode: replaceMode })}
        >
          <SegmentedControl
            fullWidth
            value={replaceMode}
            onChange={(v) => setReplaceMode(v as 'new-id' | 'same-id')}
            data={[
              { value: 'new-id', label: 'New node id' },
              { value: 'same-id', label: 'Same node id' },
            ]}
          />
          <Text size="sm" c="dimmed">
            {replaceMode === 'new-id'
              ? 'Provisions a new broker with a new id, moves the replicas to it and unregisters the old broker.'
              : 'Rebuilds the broker with the same id on a new node; its replicas are copied back from the leaders.'}
          </Text>
        </ParamsModal>
      )}
    </Stack>
  );
}
