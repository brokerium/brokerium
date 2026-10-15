// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import {
  Alert,
  Badge,
  Button,
  Group,
  MultiSelect,
  NumberInput,
  Paper,
  Select,
  SimpleGrid,
  Stack,
  Table,
  Text,
  TextInput,
  Title,
} from '@mantine/core';
import { IconAlertTriangle, IconArrowLeft, IconArrowRight, IconFlask, IconTrash } from '@tabler/icons-react';
import { useMemo, useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router';

import {
  type OffsetChange,
  type ResetOffsetsRequest,
  type ResetTarget,
  useDeleteGroup,
  useGroupDetail,
  useResetOffsets,
} from '../../api/clusters';
import { isApiError } from '../../api/errors';
import { confirmAction } from '../../components/confirm';
import { GatedButton } from '../../components/Gated';
import { notifySuccess } from '../../components/notify';
import { PageLoader, QueryError } from '../../components/QueryState';
import { StatCard } from '../../components/StatCard';
import { usePermissions } from '../../hooks/usePermissions';
import { capabilityAvailability, withRole } from '../../lib/capabilities';
import { formatNumber } from '../../lib/format';
import { canResetOffsets, groupStateColor } from '../../lib/groups';
import { useClusterContext } from './clusterContext';
import { Section } from './common';

const TARGETS: { value: ResetTarget; label: string }[] = [
  { value: 'earliest', label: 'Earliest offset' },
  { value: 'latest', label: 'Latest offset' },
  { value: 'timestamp', label: 'Timestamp' },
  { value: 'offset', label: 'Specific offset' },
];

function ChangesTable({ changes }: { changes: OffsetChange[] }) {
  if (changes.length === 0) {
    return (
      <Text size="sm" c="dimmed">
        No partitions would change.
      </Text>
    );
  }
  return (
    <Table withTableBorder striped verticalSpacing={4}>
      <Table.Thead>
        <Table.Tr>
          <Table.Th>Topic</Table.Th>
          <Table.Th>Partition</Table.Th>
          <Table.Th ta="right">From</Table.Th>
          <Table.Th />
          <Table.Th ta="right">To</Table.Th>
          <Table.Th ta="right">Delta</Table.Th>
        </Table.Tr>
      </Table.Thead>
      <Table.Tbody>
        {changes.map((c) => (
          <Table.Tr key={`${c.topic}-${c.partition}`}>
            <Table.Td>
              <Text size="sm" ff="monospace">
                {c.topic}
              </Text>
            </Table.Td>
            <Table.Td>{c.partition}</Table.Td>
            <Table.Td ta="right">{formatNumber(c.from)}</Table.Td>
            <Table.Td w={24}>
              <IconArrowRight size={12} />
            </Table.Td>
            <Table.Td ta="right">{formatNumber(c.to)}</Table.Td>
            <Table.Td ta="right">
              <Text size="sm" c={c.to < c.from ? 'orange.7' : c.to > c.from ? 'teal.7' : 'dimmed'}>
                {c.to - c.from > 0 ? '+' : ''}
                {formatNumber(c.to - c.from)}
              </Text>
            </Table.Td>
          </Table.Tr>
        ))}
      </Table.Tbody>
    </Table>
  );
}

export default function GroupPage() {
  const { group: groupId = '' } = useParams();
  const { clusterId, caps } = useClusterContext();
  const { user } = usePermissions();
  const navigate = useNavigate();
  const detail = useGroupDetail(clusterId, groupId);
  const reset = useResetOffsets(clusterId, groupId);
  const deleteGroup = useDeleteGroup(clusterId);

  const [topic, setTopic] = useState<string | null>(null);
  const [partitions, setPartitions] = useState<string[]>([]);
  const [target, setTarget] = useState<ResetTarget>('earliest');
  const [timestamp, setTimestamp] = useState('');
  const [offset, setOffset] = useState<number | ''>(0);
  const [dryRun, setDryRun] = useState<{ key: string; changes: OffsetChange[] } | null>(null);

  const rows = useMemo(() => detail.data?.partitions ?? [], [detail.data]);
  const topics = useMemo(() => [...new Set(rows.map((r) => r.topic))].sort(), [rows]);
  const topicPartitions = rows.filter((r) => r.topic === topic).map((r) => String(r.partition));

  const groupsPath = `/clusters/${encodeURIComponent(clusterId)}/groups`;
  const back = (
    <Button component={Link} to={groupsPath} variant="subtle" size="compact-sm" leftSection={<IconArrowLeft size={14} />}>
      All consumer groups
    </Button>
  );

  if (detail.isPending) return <PageLoader label="Loading consumer group" />;
  if (!detail.data) {
    return (
      <Stack>
        {back}
        <QueryError
          error={detail.error}
          title={isApiError(detail.error, 404) ? `Consumer group ${groupId} not found` : 'Could not load the group'}
          onRetry={() => void detail.refetch()}
        />
      </Stack>
    );
  }

  const g = detail.data;
  const manage = withRole(capabilityAvailability(caps, 'manageGroups'), user?.role);
  const resettable = canResetOffsets(g.state, g.members);
  const timestampMs = timestamp ? new Date(timestamp).getTime() : Number.NaN;

  const request = (dry: boolean): ResetOffsetsRequest | null => {
    if (!topic) return null;
    if (target === 'timestamp' && !Number.isFinite(timestampMs)) return null;
    if (target === 'offset' && offset === '') return null;
    return {
      topic,
      partitions: partitions.length > 0 ? partitions.map(Number) : undefined,
      to: target,
      timestamp: target === 'timestamp' ? timestampMs : undefined,
      offset: target === 'offset' && offset !== '' ? offset : undefined,
      dryRun: dry,
    };
  };
  const currentKey = JSON.stringify(request(true));
  const dryRunValid = dryRun !== null && dryRun.key === currentKey;

  const runDryRun = () => {
    const req = request(true);
    if (!req) return;
    reset.mutate(req, { onSuccess: (res) => setDryRun({ key: JSON.stringify(req), changes: res.offsets }) });
  };

  const apply = async () => {
    const req = request(false);
    if (!req || !dryRun) return;
    const ok = await confirmAction({
      title: 'Reset offsets',
      message: `Commit new offsets for ${dryRun.changes.length} partition(s) of ${req.topic} in group ${g.group}? Consumers will resume from the new positions.`,
      confirmLabel: 'Reset offsets',
    });
    if (!ok) return;
    reset.mutate(req, {
      onSuccess: (res) => {
        notifySuccess('Offsets reset', `${res.offsets.length} partition(s) of ${req.topic}`);
        setDryRun(null);
      },
    });
  };

  const remove = async () => {
    const ok = await confirmAction({
      title: `Delete consumer group ${g.group}`,
      message: 'The committed offsets of the group are deleted. Consumers that join later start from their auto.offset.reset position.',
      confirmLabel: 'Delete group',
      typeToConfirm: g.group,
      typeToConfirmWhat: 'group id',
    });
    if (!ok) return;
    deleteGroup.mutate(g.group, {
      onSuccess: () => {
        notifySuccess('Consumer group deleted', g.group);
        navigate(groupsPath, { replace: true });
      },
    });
  };

  return (
    <Stack gap="md">
      {back}
      <Group justify="space-between" align="flex-start">
        <Group gap="sm">
          <Title order={3} ff="monospace" style={{ wordBreak: 'break-all' }}>
            {g.group}
          </Title>
          <Badge variant="light" color={groupStateColor(g.state)}>
            {g.state || 'unknown'}
          </Badge>
        </Group>
        <GatedButton
          availability={manage}
          color="red"
          variant="light"
          leftSection={<IconTrash size={16} />}
          loading={deleteGroup.isPending}
          onClick={() => void remove()}
        >
          Delete group
        </GatedButton>
      </Group>

      {g.error && (
        <Alert color="red" icon={<IconAlertTriangle size={18} />}>
          {g.error}
        </Alert>
      )}

      <SimpleGrid cols={{ base: 2, sm: 4 }} spacing="sm">
        <StatCard label="Members" value={formatNumber(g.members)} />
        <StatCard label="Total lag" value={formatNumber(g.totalLag)} color={g.totalLag > 0 ? 'orange.7' : undefined} />
        <StatCard label="Partitions" value={formatNumber(rows.length)} />
        <StatCard
          label="Type"
          value={g.type === 'share' ? 'share group' : g.protocolType || 'consumer'}
          hint={g.type === 'share' ? 'KIP-932 queue semantics' : undefined}
        />
      </SimpleGrid>

      <Paper withBorder radius="md">
        <Table.ScrollContainer minWidth={900}>
          <Table striped highlightOnHover verticalSpacing="xs">
            <Table.Thead>
              <Table.Tr>
                <Table.Th>Topic</Table.Th>
                <Table.Th>Partition</Table.Th>
                <Table.Th ta="right">{g.type === 'share' ? 'Start offset' : 'Committed'}</Table.Th>
                <Table.Th ta="right">End offset</Table.Th>
                <Table.Th ta="right">Lag</Table.Th>
                <Table.Th>Member</Table.Th>
                <Table.Th>Client</Table.Th>
                <Table.Th>Host</Table.Th>
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {rows.map((r) => (
                <Table.Tr key={`${r.topic}-${r.partition}`}>
                  <Table.Td>
                    <Text size="sm" ff="monospace">
                      {r.topic}
                    </Text>
                  </Table.Td>
                  <Table.Td>{r.partition}</Table.Td>
                  <Table.Td ta="right">{r.committed < 0 ? '—' : formatNumber(r.committed)}</Table.Td>
                  <Table.Td ta="right">{formatNumber(r.end)}</Table.Td>
                  <Table.Td ta="right">
                    <Text size="sm" fw={r.lag > 0 ? 600 : undefined} c={r.lag > 0 ? 'orange.7' : undefined}>
                      {formatNumber(r.lag)}
                    </Text>
                  </Table.Td>
                  <Table.Td maw={220}>
                    <Text size="xs" ff="monospace" truncate="end" title={r.member}>
                      {r.member || '—'}
                    </Text>
                  </Table.Td>
                  <Table.Td>
                    <Text size="sm">{r.clientId || '—'}</Text>
                  </Table.Td>
                  <Table.Td>
                    <Text size="sm">{r.host || '—'}</Text>
                  </Table.Td>
                </Table.Tr>
              ))}
              {rows.length === 0 && (
                <Table.Tr>
                  <Table.Td colSpan={8}>
                    <Text size="sm" c="dimmed" ta="center" py="md">
                      The group has no committed offsets.
                    </Text>
                  </Table.Td>
                </Table.Tr>
              )}
            </Table.Tbody>
          </Table>
        </Table.ScrollContainer>
      </Paper>

      <Section title="Reset offsets" description="Always run a dry run first; apply commits exactly the previewed change.">
        <Stack gap="sm">
          {!resettable && (
            <Alert color="yellow" icon={<IconAlertTriangle size={18} />} variant="light">
              The group has active members ({g.state}). Kafka resets offsets only for groups without members: stop the
              consumers first.
            </Alert>
          )}
          <Group align="flex-end" gap="sm">
            <Select
              label="Topic"
              w={260}
              data={topics}
              value={topic}
              onChange={(v) => {
                setTopic(v);
                setPartitions([]);
              }}
              searchable
            />
            <MultiSelect
              label="Partitions"
              placeholder={partitions.length === 0 ? 'All partitions' : undefined}
              w={240}
              data={topicPartitions}
              value={partitions}
              onChange={setPartitions}
              disabled={!topic}
              clearable
            />
            <Select
              label="Reset to"
              w={180}
              data={TARGETS}
              value={target}
              onChange={(v) => v && setTarget(v as ResetTarget)}
              allowDeselect={false}
            />
            {target === 'timestamp' && (
              <TextInput
                label="Timestamp (local time)"
                type="datetime-local"
                step={1}
                w={230}
                value={timestamp}
                onChange={(e) => setTimestamp(e.currentTarget.value)}
              />
            )}
            {target === 'offset' && (
              <NumberInput label="Offset" w={160} min={0} value={offset} onChange={(v) => setOffset(v === '' ? '' : Number(v))} />
            )}
          </Group>
          <Group gap="xs">
            <GatedButton
              availability={manage}
              variant="light"
              leftSection={<IconFlask size={16} />}
              onClick={runDryRun}
              loading={reset.isPending && reset.variables?.dryRun === true}
              disabled={request(true) === null}
            >
              Dry run
            </GatedButton>
            <GatedButton
              availability={manage}
              color="orange"
              onClick={() => void apply()}
              loading={reset.isPending && reset.variables?.dryRun === false}
              disabled={!dryRunValid || !resettable}
            >
              Apply reset
            </GatedButton>
            {!dryRunValid && dryRun !== null && (
              <Text size="xs" c="dimmed">
                Parameters changed: run the dry run again.
              </Text>
            )}
          </Group>
          {dryRun && dryRunValid && <ChangesTable changes={dryRun.changes} />}
        </Stack>
      </Section>
    </Stack>
  );
}
