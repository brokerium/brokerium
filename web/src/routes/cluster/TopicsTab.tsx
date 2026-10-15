// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { Anchor, Badge, Group, Paper, Stack, Switch, Table, Text, TextInput, UnstyledButton } from '@mantine/core';
import { IconChevronDown, IconChevronUp, IconPlus, IconSearch, IconSelector } from '@tabler/icons-react';
import { type ReactNode, useMemo, useState } from 'react';
import { Link } from 'react-router';

import { useRates } from '../../api/clusters';
import type { Topic } from '../../api/types';
import { GatedButton } from '../../components/Gated';
import { usePermissions } from '../../hooks/usePermissions';
import { capabilityAvailability, withRole } from '../../lib/capabilities';
import { offlineCount, underReplicatedCount } from '../../lib/distribution';
import { formatBytes, formatBytesPerSecond, formatNumber } from '../../lib/format';
import { useClusterContext } from './clusterContext';
import { NoSnapshot } from './common';
import { CreateTopicModal } from './CreateTopicModal';

type SortKey = 'name' | 'partitions' | 'size' | 'urp' | 'rate';

interface Row {
  topic: Topic;
  urp: number;
  offline: number;
  rate: number | undefined;
}

function SortHeader({
  label,
  sortKey,
  sort,
  onSort,
  align,
}: {
  label: ReactNode;
  sortKey: SortKey;
  sort: { key: SortKey; desc: boolean };
  onSort: (key: SortKey) => void;
  align?: 'right';
}) {
  const active = sort.key === sortKey;
  const Icon = active ? (sort.desc ? IconChevronDown : IconChevronUp) : IconSelector;
  return (
    <Table.Th ta={align}>
      <UnstyledButton onClick={() => onSort(sortKey)}>
        <Group gap={4} wrap="nowrap" justify={align === 'right' ? 'flex-end' : 'flex-start'}>
          <Text size="sm" fw={700}>
            {label}
          </Text>
          <Icon size={14} />
        </Group>
      </UnstyledButton>
    </Table.Th>
  );
}

export default function TopicsTab() {
  const { clusterId, snapshot, caps, view } = useClusterContext();
  const { user } = usePermissions();
  const rates = useRates(clusterId);
  const [search, setSearch] = useState('');
  const [showInternal, setShowInternal] = useState(false);
  const [creating, setCreating] = useState(false);
  const [sort, setSort] = useState<{ key: SortKey; desc: boolean }>({ key: 'name', desc: false });

  const rows = useMemo<Row[]>(() => {
    const q = search.trim().toLowerCase();
    const list = (snapshot?.topics ?? [])
      .filter((t) => (showInternal || !t.internal) && (!q || t.name.toLowerCase().includes(q)))
      .map((topic) => ({
        topic,
        urp: underReplicatedCount(topic),
        offline: offlineCount(topic),
        rate: rates.data?.topics[topic.name]?.bytesInPerSec,
      }));
    const value = (r: Row): number | string => {
      switch (sort.key) {
        case 'name':
          return r.topic.name;
        case 'partitions':
          return r.topic.partitions.length;
        case 'size':
          return r.topic.sizeBytes;
        case 'urp':
          return r.urp + r.offline * 1000;
        case 'rate':
          return r.rate ?? -1;
        default: {
          const exhaustive: never = sort.key;
          return exhaustive;
        }
      }
    };
    return list.sort((a, b) => {
      const va = value(a);
      const vb = value(b);
      const cmp = typeof va === 'string' && typeof vb === 'string' ? va.localeCompare(vb) : Number(va) - Number(vb);
      return sort.desc ? -cmp : cmp;
    });
  }, [snapshot, search, showInternal, sort, rates.data]);

  if (!snapshot) return <NoSnapshot />;

  const onSort = (key: SortKey) =>
    setSort((s) => (s.key === key ? { key, desc: !s.desc } : { key, desc: key !== 'name' }));
  const internalCount = snapshot.topics.filter((t) => t.internal).length;
  const createAvailability = withRole(capabilityAvailability(caps, 'manageTopics'), user?.role);
  const base = `/clusters/${encodeURIComponent(clusterId)}/topics`;

  return (
    <Stack gap="md">
      <Group justify="space-between">
        <Group gap="md">
          <TextInput
            placeholder="Search topics"
            leftSection={<IconSearch size={14} />}
            value={search}
            onChange={(e) => setSearch(e.currentTarget.value)}
            w={280}
          />
          <Switch
            label={`Show internal topics (${internalCount})`}
            checked={showInternal}
            onChange={(e) => setShowInternal(e.currentTarget.checked)}
          />
        </Group>
        <GatedButton availability={createAvailability} leftSection={<IconPlus size={16} />} onClick={() => setCreating(true)}>
          Create topic
        </GatedButton>
      </Group>

      <Paper withBorder radius="md">
        <Table.ScrollContainer minWidth={900}>
          <Table highlightOnHover verticalSpacing="xs">
            <Table.Thead>
              <Table.Tr>
                <SortHeader label="Name" sortKey="name" sort={sort} onSort={onSort} />
                <SortHeader label="Partitions" sortKey="partitions" sort={sort} onSort={onSort} align="right" />
                <Table.Th ta="right">RF</Table.Th>
                <Table.Th ta="right">Min ISR</Table.Th>
                <SortHeader label="Size" sortKey="size" sort={sort} onSort={onSort} align="right" />
                {rates.data && <SortHeader label="Bytes in" sortKey="rate" sort={sort} onSort={onSort} align="right" />}
                <SortHeader label="Under-replicated" sortKey="urp" sort={sort} onSort={onSort} align="right" />
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {rows.map(({ topic, urp, offline, rate }) => (
                <Table.Tr key={topic.name}>
                  <Table.Td>
                    <Group gap={6} wrap="nowrap">
                      <Anchor component={Link} to={`${base}/${encodeURIComponent(topic.name)}`} ff="monospace" size="sm">
                        {topic.name}
                      </Anchor>
                      {topic.internal && (
                        <Badge size="xs" variant="outline" color="gray">
                          internal
                        </Badge>
                      )}
                      {topic.error && (
                        <Badge size="xs" color="red" variant="light" title={topic.error}>
                          error
                        </Badge>
                      )}
                    </Group>
                  </Table.Td>
                  <Table.Td ta="right">{formatNumber(topic.partitions.length)}</Table.Td>
                  <Table.Td ta="right">{topic.replicationFactor}</Table.Td>
                  <Table.Td ta="right">
                    <Text size="sm" c={topic.minInsyncReplicas >= topic.replicationFactor ? 'yellow.8' : undefined}>
                      {topic.minInsyncReplicas || '—'}
                    </Text>
                  </Table.Td>
                  <Table.Td ta="right">{formatBytes(topic.sizeBytes)}</Table.Td>
                  {rates.data && <Table.Td ta="right">{rate === undefined ? '—' : formatBytesPerSecond(rate)}</Table.Td>}
                  <Table.Td ta="right">
                    <Group gap={4} justify="flex-end" wrap="nowrap">
                      <Text size="sm" c={urp > 0 ? 'yellow.8' : undefined} fw={urp > 0 ? 700 : undefined}>
                        {urp}
                      </Text>
                      {offline > 0 && (
                        <Badge size="xs" color="red">
                          {offline} offline
                        </Badge>
                      )}
                    </Group>
                  </Table.Td>
                </Table.Tr>
              ))}
              {rows.length === 0 && (
                <Table.Tr>
                  <Table.Td colSpan={7}>
                    <Text size="sm" c="dimmed" ta="center" py="md">
                      No topics match.
                    </Text>
                  </Table.Td>
                </Table.Tr>
              )}
            </Table.Tbody>
          </Table>
        </Table.ScrollContainer>
      </Paper>
      <Text size="xs" c="dimmed">
        {rows.length} of {snapshot.topics.length} topics
        {rates.data ? ` · rates are agentless estimates over ${rates.data.windowSeconds} s` : ''}
      </Text>

      {creating && (
        <CreateTopicModal
          opened
          onClose={() => setCreating(false)}
          clusterId={clusterId}
          brokerCount={view.health.brokersAlive}
        />
      )}
    </Stack>
  );
}
