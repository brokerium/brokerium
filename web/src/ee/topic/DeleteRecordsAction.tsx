// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

import { Alert, Button, Group, Modal, NumberInput, SegmentedControl, Stack, Table, Text, TextInput } from '@mantine/core';
import { IconEraser } from '@tabler/icons-react';
import { useState } from 'react';

import { useTopicDetail } from '../../api/topics';
import { EnterpriseBadge } from '../../components/badges';
import { confirmAction } from '../../components/confirm';
import { GatedButton } from '../../components/Gated';
import { notifySuccess } from '../../components/notify';
import { useFeature } from '../../hooks/useFeature';
import { usePermissions } from '../../hooks/usePermissions';
import { capabilityAvailability, withRole } from '../../lib/capabilities';
import { formatNumber } from '../../lib/format';
import { useClusterContext } from '../../routes/cluster/clusterContext';
import { type DeleteRecordsRequest, type DeleteRecordsResult, useDeleteRecords } from '../api/messageTools';
import { MESSAGE_TOOLS } from '../features';

type Mode = 'offsets' | 'time' | 'all';

const MODES: { value: Mode; label: string }[] = [
  { value: 'offsets', label: 'Before offsets' },
  { value: 'time', label: 'Before a time' },
  { value: 'all', label: 'All records' },
];

interface TopicProps {
  clusterId: string;
  topic: string;
}

export function DeleteRecordsAction({ clusterId, topic }: TopicProps) {
  const { caps } = useClusterContext();
  const { user } = usePermissions();
  const { availability: licensed } = useFeature(MESSAGE_TOOLS);
  const availability = licensed.allowed ? withRole(capabilityAvailability(caps, 'manageTopics'), user?.role) : licensed;
  const [opened, setOpened] = useState(false);
  return (
    <>
      <GatedButton
        availability={availability}
        variant="default"
        leftSection={<IconEraser size={16} />}
        rightSection={licensed.allowed ? undefined : <EnterpriseBadge size="xs" />}
        onClick={() => setOpened(true)}
      >
        Delete records
      </GatedButton>
      {opened && <DeleteRecordsModal clusterId={clusterId} topic={topic} onClose={() => setOpened(false)} />}
    </>
  );
}

function deletable(policy: string | undefined): boolean {
  return (policy ?? 'delete').split(',').some((p) => p.trim() === 'delete');
}

function DeleteRecordsModal({ clusterId, topic, onClose }: TopicProps & { onClose: () => void }) {
  const detail = useTopicDetail(clusterId, topic);
  const del = useDeleteRecords(clusterId, topic);
  const [mode, setMode] = useState<Mode>('offsets');
  const [offsets, setOffsets] = useState<Record<number, number | ''>>({});
  const [time, setTime] = useState('');
  const [result, setResult] = useState<DeleteRecordsResult | null>(null);

  const partitions = Object.entries(detail.data?.offsets ?? {})
    .map(([p, o]) => ({ partition: Number(p), start: o.start ?? 0, end: o.end ?? 0 }))
    .sort((a, b) => a.partition - b.partition);
  const policy = detail.data?.configs.find((c) => c.name === 'cleanup.policy')?.value;
  const compacted = !deletable(policy);

  const selection = (): Omit<DeleteRecordsRequest, 'dryRun' | 'confirm'> | null => {
    switch (mode) {
      case 'all':
        return { all: true };
      case 'time': {
        const ms = new Date(time).getTime();
        return Number.isFinite(ms) ? { beforeTimestamp: ms } : null;
      }
      case 'offsets': {
        const list = Object.entries(offsets)
          .filter(([, offset]) => offset !== '')
          .map(([partition, offset]) => ({ partition: Number(partition), offset: Number(offset) }));
        return list.length > 0 ? { partitions: list } : null;
      }
      default: {
        const unknown: never = mode;
        return unknown;
      }
    }
  };
  const chosen = selection();
  const preview = result?.dryRun ? result : null;

  const change = (update: () => void) => {
    update();
    setResult(null);
  };

  const runPreview = () => {
    if (chosen) del.mutate({ ...chosen, dryRun: true }, { onSuccess: setResult });
  };

  const runDelete = async () => {
    if (!chosen || !preview) return;
    const ok = await confirmAction({
      title: `Delete records of ${topic}`,
      message: `${formatNumber(preview.records)} records in ${preview.partitions.length} partitions are deleted permanently. Consumers whose committed offsets fall before the new start offsets reset according to auto.offset.reset.`,
      confirmLabel: 'Delete records',
      typeToConfirm: topic,
      typeToConfirmWhat: 'topic name',
    });
    if (!ok) return;
    del.mutate(
      { ...chosen, dryRun: false, confirm: topic },
      {
        onSuccess: (res) => {
          setResult(res);
          if (res.failed === 0) {
            notifySuccess('Records deleted', `${formatNumber(res.records)} records of ${topic}`);
            onClose();
          }
        },
      },
    );
  };

  return (
    <Modal opened onClose={onClose} size="lg" centered title={<Text fw={600}>Delete records of {topic}</Text>}>
      <Stack>
        {compacted && (
          <Alert color="yellow" title="Compacted topic">
            Kafka deletes records only in topics whose cleanup.policy includes delete. Produce tombstones to remove keys
            from a compacted topic.
          </Alert>
        )}
        <SegmentedControl fullWidth data={MODES} value={mode} onChange={(v) => change(() => setMode(v as Mode))} />
        {mode === 'offsets' && (
          <Table withTableBorder verticalSpacing={4}>
            <Table.Thead>
              <Table.Tr>
                <Table.Th>Partition</Table.Th>
                <Table.Th>Start offset</Table.Th>
                <Table.Th>End offset</Table.Th>
                <Table.Th>Delete before</Table.Th>
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {partitions.map((p) => (
                <Table.Tr key={p.partition}>
                  <Table.Td>{p.partition}</Table.Td>
                  <Table.Td>{formatNumber(p.start)}</Table.Td>
                  <Table.Td>{formatNumber(p.end)}</Table.Td>
                  <Table.Td>
                    <NumberInput
                      size="xs"
                      w={140}
                      min={p.start}
                      max={p.end}
                      placeholder="Keep"
                      aria-label={`Delete records of partition ${p.partition} before offset`}
                      value={offsets[p.partition] ?? ''}
                      onChange={(v) =>
                        change(() => setOffsets({ ...offsets, [p.partition]: v === '' ? '' : Number(v) }))
                      }
                    />
                  </Table.Td>
                </Table.Tr>
              ))}
            </Table.Tbody>
          </Table>
        )}
        {mode === 'time' && (
          <TextInput
            label="Delete records older than (local time)"
            type="datetime-local"
            step={1}
            value={time}
            onChange={(e) => {
              const value = e.currentTarget.value;
              change(() => setTime(value));
            }}
          />
        )}
        {mode === 'all' && (
          <Text size="sm">
            Empties the topic: every record in every partition is deleted. The topic, its configs and its partitions stay.
          </Text>
        )}
        {result && (
          <Stack gap="xs">
            <Text size="sm" fw={500}>
              {result.dryRun
                ? `Deletes ${formatNumber(result.records)} records`
                : `Deleted ${formatNumber(result.records)} records; ${result.failed} partitions failed`}
            </Text>
            {result.partitions.length > 0 && (
              <Table withTableBorder verticalSpacing={4}>
                <Table.Thead>
                  <Table.Tr>
                    <Table.Th>Partition</Table.Th>
                    <Table.Th>Records</Table.Th>
                    <Table.Th>New start offset</Table.Th>
                  </Table.Tr>
                </Table.Thead>
                <Table.Tbody>
                  {result.partitions.map((p) => (
                    <Table.Tr key={p.partition}>
                      <Table.Td>{p.partition}</Table.Td>
                      <Table.Td>{formatNumber(p.records)}</Table.Td>
                      <Table.Td>
                        {p.error ? (
                          <Text size="sm" c="red">
                            {p.error}
                          </Text>
                        ) : (
                          formatNumber(p.deleteBefore)
                        )}
                      </Table.Td>
                    </Table.Tr>
                  ))}
                </Table.Tbody>
              </Table>
            )}
          </Stack>
        )}
        <Group justify="flex-end">
          <Button variant="default" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="default" onClick={runPreview} disabled={!chosen || compacted} loading={del.isPending && !preview}>
            Preview
          </Button>
          <Button
            color="red"
            onClick={() => void runDelete()}
            disabled={!preview || preview.records === 0}
            loading={del.isPending && preview !== null}
          >
            Delete records
          </Button>
        </Group>
      </Stack>
    </Modal>
  );
}
