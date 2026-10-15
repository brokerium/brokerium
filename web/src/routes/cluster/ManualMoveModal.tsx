// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { ActionIcon, Alert, Button, Group, Modal, MultiSelect, NumberInput, Select, Stack, Table, Text } from '@mantine/core';
import { IconInfoCircle, IconPlus, IconTrash } from '@tabler/icons-react';
import { useMemo, useState } from 'react';

import type { Snapshot } from '../../api/types';
import { usePlanner } from '../../components/operations/plannerContext';
import { ReplicaList } from '../../components/ReplicaList';

interface Move {
  topic: string;
  partition: number;
  replicas: number[];
  current: number[];
}

const MiB = 1024 * 1024;

export function ManualMoveModal({ opened, onClose, snapshot }: { opened: boolean; onClose: () => void; snapshot: Snapshot }) {
  const planner = usePlanner();
  const [topic, setTopic] = useState<string | null>(null);
  const [partition, setPartition] = useState<string | null>(null);
  const [target, setTarget] = useState<string[]>([]);
  const [moves, setMoves] = useState<Move[]>([]);
  const [throttle, setThrottle] = useState<number | ''>('');

  const topics = useMemo(() => [...snapshot.topics].sort((a, b) => a.name.localeCompare(b.name)), [snapshot.topics]);
  const selectedTopic = topics.find((t) => t.name === topic);
  const selectedPartition = selectedTopic?.partitions.find((p) => String(p.id) === partition);
  const brokerOptions = [...snapshot.brokers]
    .sort((a, b) => a.id - b.id)
    .map((b) => ({ value: String(b.id), label: `${b.id}${b.rack ? ` (${b.rack})` : ''}${b.alive ? '' : ' offline'}` }));

  const pickPartition = (value: string | null) => {
    setPartition(value);
    const p = selectedTopic?.partitions.find((x) => String(x.id) === value);
    setTarget(p ? p.replicas.map(String) : []);
  };

  const addMove = () => {
    if (!selectedTopic || !selectedPartition || target.length === 0) return;
    const move: Move = {
      topic: selectedTopic.name,
      partition: selectedPartition.id,
      replicas: target.map(Number),
      current: selectedPartition.replicas,
    };
    setMoves((prev) => [...prev.filter((m) => !(m.topic === move.topic && m.partition === move.partition)), move]);
    setPartition(null);
    setTarget([]);
  };

  const unchanged =
    selectedPartition !== undefined && target.join(',') === selectedPartition.replicas.map(String).join(',');
  const rfChange = selectedPartition !== undefined && target.length > 0 && target.length !== selectedPartition.replicas.length;

  const preview = async () => {
    const ok = await planner.plan(
      'move-partitions',
      {
        moves: moves.map(({ topic: t, partition: p, replicas }) => ({ topic: t, partition: p, replicas })),
        throttleBytesPerSec: throttle === '' ? undefined : Math.round(throttle * MiB),
      },
      { key: 'move-partitions' },
    );
    if (ok) {
      setMoves([]);
      onClose();
    }
  };

  return (
    <Modal opened={opened} onClose={onClose} title={<Text fw={600}>Manual partition move</Text>} size="lg">
      <Stack>
        <Group grow align="flex-start">
          <Select
            label="Topic"
            searchable
            data={topics.map((t) => t.name)}
            value={topic}
            onChange={(v) => {
              setTopic(v);
              setPartition(null);
              setTarget([]);
            }}
            nothingFoundMessage="No such topic"
          />
          <Select
            label="Partition"
            searchable
            disabled={!selectedTopic}
            data={(selectedTopic?.partitions ?? []).map((p) => String(p.id))}
            value={partition}
            onChange={pickPartition}
          />
        </Group>
        {selectedPartition && (
          <Group gap="xs">
            <Text size="sm" c="dimmed">
              Current replicas:
            </Text>
            <ReplicaList
              replicas={selectedPartition.replicas}
              isr={selectedPartition.isr}
              leader={selectedPartition.leader}
            />
          </Group>
        )}
        <MultiSelect
          label="Target replicas"
          description="In order: the first broker is the preferred leader"
          data={brokerOptions}
          value={target}
          onChange={setTarget}
          disabled={!selectedPartition}
          searchable
        />
        {rfChange && (
          <Text size="xs" c="yellow.8">
            The replica count changes from {selectedPartition?.replicas.length} to {target.length}: this changes the
            replication factor of the partition.
          </Text>
        )}
        <Group justify="flex-end">
          <Button
            variant="light"
            leftSection={<IconPlus size={14} />}
            onClick={addMove}
            disabled={!selectedPartition || target.length === 0 || unchanged}
          >
            Add to plan
          </Button>
        </Group>

        {moves.length > 0 ? (
          <Table withTableBorder verticalSpacing={6}>
            <Table.Thead>
              <Table.Tr>
                <Table.Th>Topic</Table.Th>
                <Table.Th>Partition</Table.Th>
                <Table.Th>From</Table.Th>
                <Table.Th>To</Table.Th>
                <Table.Th w={40} />
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {moves.map((m) => (
                <Table.Tr key={`${m.topic}-${m.partition}`}>
                  <Table.Td>
                    <Text size="sm" ff="monospace">
                      {m.topic}
                    </Text>
                  </Table.Td>
                  <Table.Td>{m.partition}</Table.Td>
                  <Table.Td>
                    <ReplicaList replicas={m.current} leader={m.current[0]} />
                  </Table.Td>
                  <Table.Td>
                    <ReplicaList
                      replicas={m.replicas}
                      leader={m.replicas[0]}
                      added={m.replicas.filter((id) => !m.current.includes(id))}
                    />
                  </Table.Td>
                  <Table.Td>
                    <ActionIcon
                      variant="subtle"
                      color="red"
                      onClick={() => setMoves(moves.filter((x) => x !== m))}
                      aria-label="Remove move"
                    >
                      <IconTrash size={14} />
                    </ActionIcon>
                  </Table.Td>
                </Table.Tr>
              ))}
            </Table.Tbody>
          </Table>
        ) : (
          <Alert color="gray" variant="light" icon={<IconInfoCircle size={18} />}>
            Pick a partition, edit its replica list and add it to the plan. Several partitions can move in one operation.
          </Alert>
        )}

        <NumberInput
          label="Replication throttle (MiB/s)"
          description="Optional; throttles are always removed when the operation ends"
          min={1}
          value={throttle}
          onChange={(v) => setThrottle(v === '' ? '' : Number(v))}
        />
        <Group justify="flex-end">
          <Button variant="default" onClick={onClose}>
            Cancel
          </Button>
          <Button onClick={() => void preview()} disabled={moves.length === 0} loading={planner.pendingKey === 'move-partitions'}>
            Preview plan
          </Button>
        </Group>
      </Stack>
    </Modal>
  );
}
