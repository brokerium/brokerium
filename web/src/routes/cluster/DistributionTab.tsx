// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import '@mantine/charts/styles.css';

import { BarChart } from '@mantine/charts';
import {
  Box,
  Button,
  Group,
  MultiSelect,
  NumberInput,
  Paper,
  SegmentedControl,
  SimpleGrid,
  Stack,
  Switch,
  Table,
  Text,
  TextInput,
  Title,
  Tooltip,
} from '@mantine/core';
import { IconArrowsShuffle, IconCrown, IconRoute, IconScale, IconSearch } from '@tabler/icons-react';
import { useMemo, useState } from 'react';

import { useRates } from '../../api/clusters';
import type { OperationParams } from '../../api/types';
import { GatedButton } from '../../components/Gated';
import { usePlanner } from '../../components/operations/plannerContext';
import { RelativeTime } from '../../components/RelativeTime';
import { heatLevel, maxReplicasPerCell, topicBrokerMatrix } from '../../lib/distribution';
import { formatBytes, formatBytesPerSecond, formatNumber, formatRate } from '../../lib/format';
import { useClusterContext } from './clusterContext';
import { NoSnapshot, ParamsModal, Section } from './common';
import { ManualMoveModal } from './ManualMoveModal';

type RebalanceMode = NonNullable<OperationParams['rebalance']['mode']>;

const HEATMAP_PAGE = 100;
const MiB = 1024 * 1024;

function BrokerChart({
  title,
  data,
  color,
  format,
}: {
  title: string;
  data: { broker: string; value: number }[];
  color: string;
  format: (v: number) => string;
}) {
  const avg = data.length ? data.reduce((s, d) => s + d.value, 0) / data.length : 0;
  return (
    <Paper withBorder radius="md" p="md">
      <Text fw={600} size="sm" mb="xs">
        {title}
      </Text>
      <BarChart
        h={220}
        data={data}
        dataKey="broker"
        series={[{ name: 'value', label: title, color }]}
        valueFormatter={format}
        tickLine="y"
        referenceLines={avg > 0 ? [{ y: avg, label: `avg ${format(avg)}`, color: 'gray.6' }] : []}
      />
    </Paper>
  );
}

export default function DistributionTab() {
  const { clusterId, snapshot } = useClusterContext();
  const planner = usePlanner();
  const rates = useRates(clusterId);
  const [showInternal, setShowInternal] = useState(false);
  const [search, setSearch] = useState('');
  const [limit, setLimit] = useState(HEATMAP_PAGE);
  const [rebalancing, setRebalancing] = useState(false);
  const [moving, setMoving] = useState(false);
  const [mode, setMode] = useState<RebalanceMode>('even');
  const [exclude, setExclude] = useState<string[]>([]);
  const [topicFilter, setTopicFilter] = useState<string[]>([]);
  const [rackAware, setRackAware] = useState(true);
  const [maxMoves, setMaxMoves] = useState<number | ''>('');
  const [throttle, setThrottle] = useState<number | ''>('');
  const [batchSize, setBatchSize] = useState<number | ''>('');

  const brokers = useMemo(() => [...(snapshot?.brokers ?? [])].sort((a, b) => a.id - b.id), [snapshot]);
  const brokerIds = brokers.map((b) => b.id);
  const matrix = useMemo(() => {
    const q = search.trim().toLowerCase();
    const topics = (snapshot?.topics ?? []).filter(
      (t) => (showInternal || !t.internal) && (!q || t.name.toLowerCase().includes(q)),
    );
    return topicBrokerMatrix(topics, brokers.map((b) => b.id));
  }, [snapshot, brokers, showInternal, search]);

  if (!snapshot) return <NoSnapshot />;

  const max = maxReplicasPerCell(matrix);
  const rateData = rates.data;
  const replicaData = brokers.map((b) => ({ broker: String(b.id), value: b.replicas }));
  const leaderData = brokers.map((b) => ({ broker: String(b.id), value: b.leaders }));
  const dataData = brokers.map((b) => ({ broker: String(b.id), value: b.dataBytes }));

  const openRebalance = () => {
    setMode('even');
    setRebalancing(true);
  };

  const planRebalance = () =>
    planner.plan(
      'rebalance',
      {
        mode,
        excludeBrokers: exclude.length > 0 ? exclude.map(Number) : undefined,
        topics: topicFilter.length > 0 ? topicFilter : undefined,
        rackAware,
        maxMoves: maxMoves === '' ? undefined : maxMoves,
        throttleBytesPerSec: throttle === '' ? undefined : Math.round(throttle * MiB),
        batchSize: batchSize === '' ? undefined : batchSize,
      },
      { key: 'rebalance' },
    );

  return (
    <Stack gap="md">
      <Group justify="space-between">
        <Text size="sm" c="dimmed">
          Replica, leader and data placement across {brokers.length} brokers. Every rebalance is planned first and runs
          throttled, in batches, behind the health gate.
        </Text>
        <Group gap="xs">
          <GatedButton
            availability={planner.availability('rebalance')}
            leftSection={<IconScale size={16} />}
            onClick={openRebalance}
          >
            Balance evenly
          </GatedButton>
          <GatedButton
            availability={planner.availability('rebalance')}
            variant="light"
            leftSection={<IconCrown size={16} />}
            loading={planner.pendingKey === 'rebalance-leaders'}
            onClick={() => void planner.plan('rebalance', { mode: 'leaders' }, { key: 'rebalance-leaders' })}
          >
            Balance leaders only
          </GatedButton>
          <GatedButton
            availability={planner.availability('elect-leaders')}
            variant="light"
            leftSection={<IconRoute size={16} />}
            loading={planner.pendingKey === 'elect-leaders'}
            onClick={() => void planner.plan('elect-leaders', {})}
          >
            Elect preferred leaders
          </GatedButton>
          <GatedButton
            availability={planner.availability('move-partitions')}
            variant="default"
            leftSection={<IconArrowsShuffle size={16} />}
            onClick={() => setMoving(true)}
          >
            Manual move
          </GatedButton>
        </Group>
      </Group>

      <SimpleGrid cols={{ base: 1, md: 3 }} spacing="md">
        <BrokerChart title="Replicas" data={replicaData} color="indigo.6" format={(v) => formatNumber(Math.round(v))} />
        <BrokerChart title="Leaders" data={leaderData} color="teal.6" format={(v) => formatNumber(Math.round(v))} />
        <BrokerChart title="Data" data={dataData} color="grape.6" format={(v) => formatBytes(v)} />
      </SimpleGrid>

      <Section
        title="Current rates"
        description={
          rateData ? (
            <>
              Agentless estimates over {rateData.windowSeconds} s · sampled{' '}
              <RelativeTime value={rateData.sampledAt} size="sm" c="dimmed" />
            </>
          ) : undefined
        }
      >
        {rateData ? (
          <Table.ScrollContainer minWidth={480}>
            <Table verticalSpacing={6} striped>
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>Broker</Table.Th>
                  <Table.Th ta="right">Bytes in</Table.Th>
                  <Table.Th ta="right">Messages in</Table.Th>
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {brokers.map((b) => {
                  const r = rateData.brokers[String(b.id)];
                  return (
                    <Table.Tr key={b.id}>
                      <Table.Td fw={600}>{b.id}</Table.Td>
                      <Table.Td ta="right">{r ? formatBytesPerSecond(r.bytesInPerSec) : '—'}</Table.Td>
                      <Table.Td ta="right">{r ? formatRate(r.messagesInPerSec, ' msg/s') : '—'}</Table.Td>
                    </Table.Tr>
                  );
                })}
              </Table.Tbody>
            </Table>
          </Table.ScrollContainer>
        ) : (
          <Text size="sm" c="dimmed">
            {rates.isPending ? 'Loading rates…' : 'Rates are not available yet for this cluster.'}
          </Text>
        )}
      </Section>

      <Section
        title="Replicas per topic and broker"
        description="Cell color shows how many replicas of the topic a broker holds; hover for leader counts."
        actions={
          <Group gap="sm">
            <TextInput
              size="xs"
              placeholder="Filter topics"
              leftSection={<IconSearch size={12} />}
              value={search}
              onChange={(e) => setSearch(e.currentTarget.value)}
            />
            <Switch size="xs" label="Internal topics" checked={showInternal} onChange={(e) => setShowInternal(e.currentTarget.checked)} />
          </Group>
        }
      >
        <Table.ScrollContainer minWidth={Math.max(480, 220 + brokerIds.length * 64)}>
          <Table withColumnBorders withTableBorder verticalSpacing={4} horizontalSpacing={6} stickyHeader>
            <Table.Thead>
              <Table.Tr>
                <Table.Th>Topic</Table.Th>
                <Table.Th ta="center">Partitions</Table.Th>
                {brokerIds.map((id) => (
                  <Table.Th key={id} ta="center">
                    {id}
                  </Table.Th>
                ))}
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {matrix.slice(0, limit).map((row) => (
                <Table.Tr key={row.topic}>
                  <Table.Td maw={260}>
                    <Text size="xs" ff="monospace" truncate="end" title={row.topic}>
                      {row.topic}
                    </Text>
                  </Table.Td>
                  <Table.Td ta="center">
                    <Text size="xs">{row.partitions}</Text>
                  </Table.Td>
                  {brokerIds.map((id) => {
                    const cell = row.cells[id] ?? { replicas: 0, leaders: 0 };
                    const level = heatLevel(cell.replicas, max);
                    return (
                      <Tooltip
                        key={id}
                        label={`${row.topic} on broker ${id}: ${cell.replicas} replicas, ${cell.leaders} leaders`}
                        withArrow
                        openDelay={200}
                      >
                        <Table.Td
                          ta="center"
                          style={{
                            background: level > 0 ? `var(--mantine-color-indigo-${Math.min(level, 9)})` : undefined,
                            color: level >= 5 ? 'var(--mantine-color-white)' : undefined,
                          }}
                        >
                          <Text size="xs" fw={cell.leaders > 0 ? 700 : 400} c={level === 0 ? 'dimmed' : undefined}>
                            {cell.replicas}
                          </Text>
                        </Table.Td>
                      </Tooltip>
                    );
                  })}
                </Table.Tr>
              ))}
            </Table.Tbody>
          </Table>
        </Table.ScrollContainer>
        {matrix.length > limit && (
          <Group justify="center" mt="xs">
            <Button variant="subtle" size="xs" onClick={() => setLimit((l) => l + HEATMAP_PAGE)}>
              Show more topics ({matrix.length - limit} hidden)
            </Button>
          </Group>
        )}
        {matrix.length === 0 && (
          <Box py="sm">
            <Text size="sm" c="dimmed" ta="center">
              No topics.
            </Text>
          </Box>
        )}
      </Section>

      <ParamsModal
        opened={rebalancing}
        onClose={() => setRebalancing(false)}
        title="Rebalance"
        description="Computes a plan with the built-in balancer. Nothing moves until you confirm the plan."
        planning={planner.pendingKey === 'rebalance'}
        onPlan={planRebalance}
      >
        <Stack gap={4}>
          <Text size="sm" fw={500}>
            Mode
          </Text>
          <SegmentedControl
            fullWidth
            value={mode}
            onChange={(v) => setMode(v as RebalanceMode)}
            data={[
              { value: 'even', label: 'Replicas and leaders' },
              { value: 'replicas', label: 'Replicas only' },
              { value: 'leaders', label: 'Leaders only' },
            ]}
          />
        </Stack>
        <MultiSelect
          label="Exclude brokers"
          description="Excluded brokers receive no new replicas (for example before a decommission)"
          data={brokerIds.map(String)}
          value={exclude}
          onChange={setExclude}
          clearable
        />
        <MultiSelect
          label="Topics"
          description="Leave empty to balance every topic"
          data={snapshot.topics.filter((t) => !t.internal).map((t) => t.name)}
          value={topicFilter}
          onChange={setTopicFilter}
          searchable
          clearable
          limit={50}
        />
        <Switch label="Rack aware" checked={rackAware} onChange={(e) => setRackAware(e.currentTarget.checked)} />
        <Group grow>
          <NumberInput label="Max moves" min={1} value={maxMoves} onChange={(v) => setMaxMoves(v === '' ? '' : Number(v))} />
          <NumberInput label="Batch size" min={1} value={batchSize} onChange={(v) => setBatchSize(v === '' ? '' : Number(v))} />
          <NumberInput
            label="Throttle (MiB/s)"
            min={1}
            value={throttle}
            onChange={(v) => setThrottle(v === '' ? '' : Number(v))}
          />
        </Group>
      </ParamsModal>

      {moving && <ManualMoveModal opened onClose={() => setMoving(false)} snapshot={snapshot} />}

      <Title order={6} c="dimmed" fw={400}>
        Bold cells contain partition leaders.
      </Title>
    </Stack>
  );
}
