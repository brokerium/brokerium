// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import {
  Accordion,
  Alert,
  Badge,
  Button,
  Group,
  SegmentedControl,
  SimpleGrid,
  Skeleton,
  Stack,
  Table,
  Text,
  Title,
} from '@mantine/core';
import { IconAlertOctagon, IconAlertTriangle, IconArrowRight, IconCrown } from '@tabler/icons-react';
import { lazy, Suspense, useState } from 'react';

import type { DistributionStats, OperationPlan, PartitionMove } from '../../api/types';
import { type BrokerComparisonRow, compareDistributions, summarizeMoves } from '../../lib/distribution';
import { formatBytes, formatNumber } from '../../lib/format';
import { hasContent } from '../../lib/messages';
import type { CompareMetric } from '../charts/DistributionCompareChart';
import { JsonBlock } from '../JsonBlock';
import { ReplicaList } from '../ReplicaList';
import { StatCard } from '../StatCard';

const DistributionCompareChart = lazy(() => import('../charts/DistributionCompareChart'));

const MOVES_PAGE = 100;

function Delta({ before, after, format }: { before?: number; after?: number; format: (v: number) => string }) {
  if (before === undefined && after === undefined) return <Text size="sm">—</Text>;
  const changed = before !== after;
  return (
    <Group gap={4} wrap="nowrap">
      <Text size="sm" c={changed ? 'dimmed' : undefined}>
        {before === undefined ? '—' : format(before)}
      </Text>
      {changed && (
        <>
          <IconArrowRight size={12} />
          <Text size="sm" fw={600}>
            {after === undefined ? '—' : format(after)}
          </Text>
        </>
      )}
    </Group>
  );
}

function spreadText(stats: DistributionStats | undefined, key: 'replicaSpread' | 'leaderSpread' | 'dataSpreadBytes') {
  if (!stats) return '—';
  return key === 'dataSpreadBytes' ? formatBytes(stats[key]) : formatNumber(stats[key]);
}

function BeforeAfter({ rows, before, after }: { rows: BrokerComparisonRow[]; before?: DistributionStats; after?: DistributionStats }) {
  const [metric, setMetric] = useState<CompareMetric>('replicas');
  const fmtNumber = (v: number) => formatNumber(v);
  const fmtBytes = (v: number) => formatBytes(v);
  return (
    <Stack gap="sm">
      <Group justify="space-between">
        <Title order={5}>Distribution before and after</Title>
        <SegmentedControl
          size="xs"
          value={metric}
          onChange={(v) => setMetric(v as CompareMetric)}
          data={[
            { value: 'replicas', label: 'Replicas' },
            { value: 'leaders', label: 'Leaders' },
            { value: 'dataBytes', label: 'Data' },
          ]}
        />
      </Group>
      <Suspense fallback={<Skeleton h={240} />}>
        <DistributionCompareChart rows={rows} metric={metric} />
      </Suspense>
      <Table.ScrollContainer minWidth={560}>
        <Table striped withTableBorder verticalSpacing={6}>
          <Table.Thead>
            <Table.Tr>
              <Table.Th>Broker</Table.Th>
              <Table.Th>Rack</Table.Th>
              <Table.Th>Replicas</Table.Th>
              <Table.Th>Leaders</Table.Th>
              <Table.Th>Data</Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {rows.map((r) => (
              <Table.Tr key={r.id}>
                <Table.Td>
                  <Group gap={6}>
                    <Text size="sm" fw={600}>
                      {r.id}
                    </Text>
                    {r.excluded && (
                      <Badge size="xs" color="orange" variant="light">
                        excluded
                      </Badge>
                    )}
                  </Group>
                </Table.Td>
                <Table.Td>{r.rack || '—'}</Table.Td>
                <Table.Td>
                  <Delta before={r.before?.replicas} after={r.after?.replicas} format={fmtNumber} />
                </Table.Td>
                <Table.Td>
                  <Delta before={r.before?.leaders} after={r.after?.leaders} format={fmtNumber} />
                </Table.Td>
                <Table.Td>
                  <Delta before={r.before?.dataBytes} after={r.after?.dataBytes} format={fmtBytes} />
                </Table.Td>
              </Table.Tr>
            ))}
          </Table.Tbody>
        </Table>
      </Table.ScrollContainer>
      <SimpleGrid cols={{ base: 2, sm: 4 }} spacing="xs">
        <Text size="xs" c="dimmed">
          Replica spread: {spreadText(before, 'replicaSpread')} → {spreadText(after, 'replicaSpread')}
        </Text>
        <Text size="xs" c="dimmed">
          Leader spread: {spreadText(before, 'leaderSpread')} → {spreadText(after, 'leaderSpread')}
        </Text>
        <Text size="xs" c="dimmed">
          Data spread: {spreadText(before, 'dataSpreadBytes')} → {spreadText(after, 'dataSpreadBytes')}
        </Text>
        <Text size="xs" c="dimmed">
          Score: {before ? before.score.toFixed(3) : '—'} → {after ? after.score.toFixed(3) : '—'}
        </Text>
      </SimpleGrid>
    </Stack>
  );
}

export function MovesTable({ moves }: { moves: PartitionMove[] }) {
  const [limit, setLimit] = useState(MOVES_PAGE);
  const shown = moves.slice(0, limit);
  return (
    <Stack gap="xs">
      <Title order={5}>Partition moves ({formatNumber(moves.length)})</Title>
      <Table.ScrollContainer minWidth={720}>
        <Table striped withTableBorder verticalSpacing={6}>
          <Table.Thead>
            <Table.Tr>
              <Table.Th>Topic</Table.Th>
              <Table.Th>Partition</Table.Th>
              <Table.Th>From</Table.Th>
              <Table.Th>To</Table.Th>
              <Table.Th>Size</Table.Th>
              <Table.Th>Replica moves</Table.Th>
              <Table.Th>Leader</Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {shown.map((m) => (
              <Table.Tr key={`${m.topic}-${m.partition}`}>
                <Table.Td>
                  <Text size="sm" ff="monospace">
                    {m.topic}
                  </Text>
                </Table.Td>
                <Table.Td>{m.partition}</Table.Td>
                <Table.Td>
                  <ReplicaList replicas={m.from} leader={m.from[0]} />
                </Table.Td>
                <Table.Td>
                  <ReplicaList replicas={m.to} leader={m.to[0]} added={m.to.filter((id) => !m.from.includes(id))} />
                </Table.Td>
                <Table.Td>{formatBytes(m.sizeBytes)}</Table.Td>
                <Table.Td>{m.replicaMoves}</Table.Td>
                <Table.Td>{m.leaderChange ? <IconCrown size={16} color="var(--mantine-color-yellow-6)" /> : null}</Table.Td>
              </Table.Tr>
            ))}
          </Table.Tbody>
        </Table>
      </Table.ScrollContainer>
      {moves.length > limit && (
        <Group justify="center">
          <Button variant="subtle" size="xs" onClick={() => setLimit((l) => l + MOVES_PAGE * 5)}>
            Show more ({formatNumber(moves.length - limit)} hidden)
          </Button>
        </Group>
      )}
    </Stack>
  );
}

interface PlanPreviewProps {
  plan: OperationPlan | undefined;
  irreversible: boolean;
}

/** Plan of an operation: summary, warnings, numbers, before/after distribution and moves. */
export function PlanPreview({ plan, irreversible }: PlanPreviewProps) {
  const moves = plan?.moves ?? [];
  const summary = summarizeMoves(moves);
  const rows = compareDistributions(plan?.before, plan?.after);
  const warnings = plan?.warnings ?? [];
  return (
    <Stack gap="md">
      {plan?.summary && <Text>{plan.summary}</Text>}
      {irreversible && (
        <Alert color="red" icon={<IconAlertOctagon size={18} />} title="Irreversible operation">
          This operation cannot be undone. Starting it requires typing the cluster name.
        </Alert>
      )}
      {warnings.map((w) => (
        <Alert key={w} color="yellow" icon={<IconAlertTriangle size={18} />} variant="light">
          {w}
        </Alert>
      ))}
      {(moves.length > 0 || (plan?.dataToMoveBytes ?? 0) > 0) && (
        <SimpleGrid cols={{ base: 2, sm: 4 }} spacing="sm">
          <StatCard label="Partitions moved" value={formatNumber(summary.partitions)} />
          <StatCard label="Replica moves" value={formatNumber(summary.replicaMoves)} />
          <StatCard label="Leader changes" value={formatNumber(summary.leaderChanges)} />
          <StatCard label="Data to move" value={formatBytes(plan?.dataToMoveBytes ?? summary.bytes)} />
        </SimpleGrid>
      )}
      {rows.length > 0 && <BeforeAfter rows={rows} before={plan?.before} after={plan?.after} />}
      {moves.length > 0 && <MovesTable moves={moves} />}
      {hasContent(plan?.details) && (
        <Accordion variant="contained">
          <Accordion.Item value="details">
            <Accordion.Control>Plan details</Accordion.Control>
            <Accordion.Panel>
              <JsonBlock value={plan?.details} />
            </Accordion.Panel>
          </Accordion.Item>
        </Accordion>
      )}
    </Stack>
  );
}
