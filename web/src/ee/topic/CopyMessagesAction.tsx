// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

import { Button, Group, Modal, NumberInput, SegmentedControl, Select, Stack, Switch, Text, TextInput } from '@mantine/core';
import { IconCopy } from '@tabler/icons-react';
import { useState } from 'react';
import { useNavigate } from 'react-router';

import { useOverview } from '../../api/clusters';
import { useTopicDetail } from '../../api/topics';
import { EnterpriseBadge } from '../../components/badges';
import { GatedButton } from '../../components/Gated';
import { useFeature } from '../../hooks/useFeature';
import { usePermissions } from '../../hooks/usePermissions';
import { capabilityAvailability, withRole } from '../../lib/capabilities';
import { useClusterContext } from '../../routes/cluster/clusterContext';
import { usePlanCopy } from '../api/messageTools';
import { MESSAGE_TOOLS } from '../features';
import { EMPTY_FILTER, type FilterState, filterRequest } from './filter';
import { FilterFields } from './FilterFields';

type Partitioning = 'auto' | 'same' | 'key';

const PARTITIONING: { value: Partitioning; label: string }[] = [
  { value: 'auto', label: 'Automatic' },
  { value: 'same', label: 'Same partition' },
  { value: 'key', label: 'By key' },
];

interface TopicProps {
  clusterId: string;
  topic: string;
}

export function CopyMessagesAction({ clusterId, topic }: TopicProps) {
  const { caps } = useClusterContext();
  const { user } = usePermissions();
  const { availability: licensed } = useFeature(MESSAGE_TOOLS);
  const availability = licensed.allowed ? withRole(capabilityAvailability(caps, 'browseMessages'), user?.role) : licensed;
  const [opened, setOpened] = useState(false);
  return (
    <>
      <GatedButton
        availability={availability}
        variant="default"
        leftSection={<IconCopy size={16} />}
        rightSection={licensed.allowed ? undefined : <EnterpriseBadge size="xs" />}
        onClick={() => setOpened(true)}
      >
        Copy messages
      </GatedButton>
      {opened && <CopyMessagesModal clusterId={clusterId} topic={topic} onClose={() => setOpened(false)} />}
    </>
  );
}

function CopyMessagesModal({ clusterId, topic, onClose }: TopicProps & { onClose: () => void }) {
  const navigate = useNavigate();
  const overview = useOverview();
  const detail = useTopicDetail(clusterId, topic);
  const plan = usePlanCopy(clusterId);
  const [filter, setFilter] = useState<FilterState>(EMPTY_FILTER);
  const [targetCluster, setTargetCluster] = useState(clusterId);
  const [targetTopic, setTargetTopic] = useState('');
  const [partitioning, setPartitioning] = useState<Partitioning>('auto');
  const [keepTimestamps, setKeepTimestamps] = useState(true);
  const [throttle, setThrottle] = useState<number | ''>(10);

  const clusters = (overview.data ?? [])
    .filter((c) => c.capabilities.browseMessages)
    .map((c) => ({ value: c.cluster.id, label: c.cluster.name }));
  const partitionCount = detail.data?.topic.partitions.length ?? 0;

  const submit = () => {
    plan.mutate(
      {
        ...filterRequest(filter),
        topic,
        targetCluster: targetCluster === clusterId ? undefined : targetCluster,
        targetTopic: targetTopic.trim(),
        partitioning: partitioning === 'auto' ? undefined : partitioning,
        keepTimestamps,
        throttleBytesPerSec: throttle === '' ? undefined : Math.round(throttle * 1024 * 1024),
      },
      { onSuccess: (op) => navigate(`/operations/${encodeURIComponent(op.id)}`) },
    );
  };

  return (
    <Modal opened onClose={onClose} size="xl" centered title={<Text fw={600}>Copy messages of {topic}</Text>}>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          submit();
        }}
      >
        <Stack>
          <Text size="sm" c="dimmed">
            Copies the records that exist now and match the filter. Brokerium plans the copy first; it starts once you
            confirm it on the operation page.
          </Text>
          <Group grow align="flex-start">
            <Select
              label="Target cluster"
              data={clusters}
              value={targetCluster}
              allowDeselect={false}
              searchable
              onChange={(v) => setTargetCluster(v ?? clusterId)}
            />
            <TextInput
              label="Target topic"
              description="It must exist; the same topic replays the records"
              required
              value={targetTopic}
              onChange={(e) => setTargetTopic(e.currentTarget.value)}
            />
          </Group>
          <FilterFields value={filter} onChange={setFilter} partitionCount={partitionCount} />
          <Group align="flex-end" gap="lg">
            <Stack gap={4}>
              <Text size="sm" fw={500}>
                Partitioning
              </Text>
              <SegmentedControl data={PARTITIONING} value={partitioning} onChange={(v) => setPartitioning(v as Partitioning)} />
            </Stack>
            <NumberInput
              label="Throttle (MiB/s)"
              w={150}
              min={0.1}
              step={1}
              value={throttle}
              onChange={(v) => setThrottle(v === '' ? '' : Number(v))}
            />
            <Switch
              mb={8}
              label="Keep timestamps"
              checked={keepTimestamps}
              onChange={(e) => setKeepTimestamps(e.currentTarget.checked)}
            />
          </Group>
          <Group justify="flex-end">
            <Button variant="default" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" disabled={!targetTopic.trim()} loading={plan.isPending}>
              Plan copy
            </Button>
          </Group>
        </Stack>
      </form>
    </Modal>
  );
}
