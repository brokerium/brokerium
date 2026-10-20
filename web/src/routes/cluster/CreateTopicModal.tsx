// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { ActionIcon, Autocomplete, Button, Group, Modal, NumberInput, Stack, Text, TextInput } from '@mantine/core';
import { IconPlus, IconTrash } from '@tabler/icons-react';
import { useState } from 'react';

import { useCreateTopic } from '../../api/topics';
import { notifySuccess } from '../../components/notify';
import { topicNameError } from '../../lib/validation';

const COMMON_TOPIC_CONFIGS = [
  'cleanup.policy',
  'retention.ms',
  'retention.bytes',
  'min.insync.replicas',
  'segment.bytes',
  'segment.ms',
  'compression.type',
  'max.message.bytes',
  'message.timestamp.type',
  'unclean.leader.election.enable',
];

interface ConfigRow {
  id: number;
  key: string;
  value: string;
}

interface CreateTopicModalProps {
  opened: boolean;
  onClose: () => void;
  clusterId: string;
  brokerCount: number;
}

export function CreateTopicModal({ opened, onClose, clusterId, brokerCount }: CreateTopicModalProps) {
  const create = useCreateTopic(clusterId);
  const [name, setName] = useState('');
  // An empty field leaves the value to the broker defaults (num.partitions, default.replication.factor).
  const [partitions, setPartitions] = useState<number | ''>(3);
  const [replicationFactor, setReplicationFactor] = useState<number | ''>(Math.max(1, Math.min(3, brokerCount)));
  const [rows, setRows] = useState<ConfigRow[]>([]);
  const [nextId, setNextId] = useState(1);
  const [submitted, setSubmitted] = useState(false);

  const nameError = submitted ? topicNameError(name.trim()) : null;
  const rfError =
    replicationFactor !== '' && replicationFactor > brokerCount ? `Only ${brokerCount} brokers are available` : null;

  const submit = () => {
    setSubmitted(true);
    if (topicNameError(name.trim()) || rfError) return;
    const configs: Record<string, string> = {};
    for (const row of rows) {
      if (row.key.trim()) configs[row.key.trim()] = row.value;
    }
    create.mutate(
      {
        name: name.trim(),
        partitions: partitions === '' ? 0 : partitions,
        replicationFactor: replicationFactor === '' ? 0 : replicationFactor,
        configs: Object.keys(configs).length > 0 ? configs : undefined,
      },
      {
        onSuccess: () => {
          notifySuccess('Topic created', name.trim());
          onClose();
        },
      },
    );
  };

  return (
    <Modal opened={opened} onClose={onClose} title={<Text fw={600}>Create topic</Text>} size="lg">
      <form
        onSubmit={(e) => {
          e.preventDefault();
          submit();
        }}
      >
        <Stack>
          <TextInput
            label="Name"
            required
            value={name}
            onChange={(e) => setName(e.currentTarget.value)}
            error={nameError}
            data-autofocus
          />
          <Group grow>
            <NumberInput
              label="Partitions"
              placeholder="Broker default"
              min={1}
              max={10000}
              allowDecimal={false}
              value={partitions}
              onChange={(v) => setPartitions(v === '' ? '' : Number(v))}
            />
            <NumberInput
              label="Replication factor"
              placeholder="Broker default"
              min={1}
              max={Math.max(1, brokerCount)}
              allowDecimal={false}
              value={replicationFactor}
              onChange={(v) => setReplicationFactor(v === '' ? '' : Number(v))}
              error={rfError}
            />
          </Group>
          <Stack gap="xs">
            <Group justify="space-between">
              <Text size="sm" fw={500}>
                Configs
              </Text>
              <Button
                size="compact-xs"
                variant="subtle"
                leftSection={<IconPlus size={12} />}
                onClick={() => {
                  setRows([...rows, { id: nextId, key: '', value: '' }]);
                  setNextId(nextId + 1);
                }}
              >
                Add config
              </Button>
            </Group>
            {rows.length === 0 && (
              <Text size="xs" c="dimmed">
                Broker defaults apply. Add overrides such as retention.ms or cleanup.policy.
              </Text>
            )}
            {rows.map((row) => (
              <Group key={row.id} gap="xs" wrap="nowrap" align="flex-start">
                <Autocomplete
                  placeholder="config name"
                  data={COMMON_TOPIC_CONFIGS}
                  value={row.key}
                  onChange={(key) => setRows(rows.map((r) => (r.id === row.id ? { ...r, key } : r)))}
                  style={{ flex: 1 }}
                />
                <TextInput
                  placeholder="value"
                  value={row.value}
                  onChange={(e) => {
                    const value = e.currentTarget.value;
                    setRows(rows.map((r) => (r.id === row.id ? { ...r, value } : r)));
                  }}
                  style={{ flex: 1 }}
                />
                <ActionIcon
                  variant="subtle"
                  color="red"
                  mt={4}
                  onClick={() => setRows(rows.filter((r) => r.id !== row.id))}
                  aria-label="Remove config"
                >
                  <IconTrash size={14} />
                </ActionIcon>
              </Group>
            ))}
          </Stack>
          <Group justify="flex-end">
            <Button variant="default" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" loading={create.isPending}>
              Create topic
            </Button>
          </Group>
        </Stack>
      </form>
    </Modal>
  );
}
