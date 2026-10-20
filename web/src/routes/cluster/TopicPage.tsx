// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { Badge, Group, Modal, NumberInput, SimpleGrid, Stack, Tabs, Text, Title, Button } from '@mantine/core';
import { IconArrowLeft, IconPlus, IconTrash } from '@tabler/icons-react';
import { topicActions } from '@ee';
import { useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router';

import { isApiError } from '../../api/errors';
import { useAddPartitions, useAlterTopicConfigs, useDeleteTopic, useTopicDetail } from '../../api/topics';
import { confirmAction } from '../../components/confirm';
import { ConfigTable } from '../../components/ConfigTable';
import { CopyText } from '../../components/CopyText';
import { GatedButton } from '../../components/Gated';
import { notifySuccess } from '../../components/notify';
import { PageLoader, QueryError } from '../../components/QueryState';
import { StatCard } from '../../components/StatCard';
import { usePermissions } from '../../hooks/usePermissions';
import { capabilityAvailability, withRole } from '../../lib/capabilities';
import { underReplicatedCount } from '../../lib/distribution';
import { formatBytes, formatNumber } from '../../lib/format';
import { useClusterContext } from './clusterContext';
import { TabNotAvailable } from './common';
import { MessagesPanel } from './topic/MessagesPanel';
import { PartitionsPanel } from './topic/PartitionsPanel';
import { ProducePanel } from './topic/ProducePanel';

export default function TopicPage() {
  const { topic: topicName = '' } = useParams();
  const { clusterId, caps } = useClusterContext();
  const { user } = usePermissions();
  const navigate = useNavigate();
  const detail = useTopicDetail(clusterId, topicName);
  const alter = useAlterTopicConfigs(clusterId, topicName);
  const addPartitions = useAddPartitions(clusterId, topicName);
  const deleteTopic = useDeleteTopic(clusterId);
  const [tab, setTab] = useState<string | null>('partitions');
  const [adding, setAdding] = useState(false);
  const [newCount, setNewCount] = useState<number>(0);

  const topicsPath = `/clusters/${encodeURIComponent(clusterId)}/topics`;
  const back = (
    <Button component={Link} to={topicsPath} variant="subtle" size="compact-sm" leftSection={<IconArrowLeft size={14} />}>
      All topics
    </Button>
  );

  if (detail.isPending) return <PageLoader label="Loading topic" />;
  if (!detail.data) {
    return (
      <Stack>
        {back}
        <QueryError
          error={detail.error}
          title={isApiError(detail.error, 404) ? `Topic ${topicName} not found` : 'Could not load the topic'}
          onRetry={() => void detail.refetch()}
        />
      </Stack>
    );
  }

  const { topic, configs } = detail.data;
  const partitionCount = topic.partitions.length;
  const manageTopics = withRole(capabilityAvailability(caps, 'manageTopics'), user?.role);
  const manageConfigs = withRole(capabilityAvailability(caps, 'manageConfigs'), user?.role);
  const produceAvailability = withRole(capabilityAvailability(caps, 'browseMessages'), user?.role);
  const urp = underReplicatedCount(topic);

  const remove = async () => {
    const ok = await confirmAction({
      title: `Delete topic ${topic.name}`,
      message: 'All records of this topic are deleted permanently. Producers and consumers of the topic will fail.',
      confirmLabel: 'Delete topic',
      typeToConfirm: topic.name,
      typeToConfirmWhat: 'topic name',
    });
    if (!ok) return;
    deleteTopic.mutate(topic.name, {
      onSuccess: () => {
        notifySuccess('Topic deleted', topic.name);
        navigate(topicsPath, { replace: true });
      },
    });
  };

  return (
    <Stack gap="md">
      {back}
      <Group justify="space-between" align="flex-start">
        <Stack gap={4}>
          <Group gap="sm">
            <Title order={3} ff="monospace" style={{ wordBreak: 'break-all' }}>
              {topic.name}
            </Title>
            {topic.internal && (
              <Badge variant="outline" color="gray">
                internal
              </Badge>
            )}
          </Group>
          {topic.id && (
            <Group gap={4}>
              <Text size="sm" c="dimmed">
                Topic ID
              </Text>
              <CopyText value={topic.id} size="sm" c="dimmed" />
            </Group>
          )}
        </Stack>
        <Group gap="xs">
          {topicActions.map(({ label, Component }) => (
            <Component key={label} clusterId={clusterId} topic={topic.name} />
          ))}
          <GatedButton
            availability={manageTopics}
            variant="default"
            leftSection={<IconPlus size={16} />}
            onClick={() => {
              setNewCount(partitionCount + 1);
              setAdding(true);
            }}
          >
            Add partitions
          </GatedButton>
          <GatedButton
            availability={manageTopics}
            color="red"
            variant="light"
            leftSection={<IconTrash size={16} />}
            loading={deleteTopic.isPending}
            onClick={() => void remove()}
          >
            Delete
          </GatedButton>
        </Group>
      </Group>

      <SimpleGrid cols={{ base: 2, sm: 5 }} spacing="sm">
        <StatCard label="Partitions" value={formatNumber(partitionCount)} />
        <StatCard label="Replication factor" value={topic.replicationFactor} />
        <StatCard label="Min ISR" value={topic.minInsyncReplicas || '—'} />
        <StatCard label="Size" value={formatBytes(topic.sizeBytes)} />
        <StatCard label="Under-replicated" value={urp} color={urp > 0 ? 'yellow.7' : undefined} />
      </SimpleGrid>

      <Tabs value={tab} onChange={setTab} keepMounted={false}>
        <Tabs.List>
          <Tabs.Tab value="partitions">Partitions</Tabs.Tab>
          <Tabs.Tab value="configs">Configs</Tabs.Tab>
          <Tabs.Tab value="messages">Messages</Tabs.Tab>
          <Tabs.Tab value="produce">Produce</Tabs.Tab>
        </Tabs.List>
        <Tabs.Panel value="partitions" pt="md">
          <PartitionsPanel detail={detail.data} />
        </Tabs.Panel>
        <Tabs.Panel value="configs" pt="md">
          <ConfigTable
            configs={configs}
            overrideSource="DYNAMIC_TOPIC_CONFIG"
            editable={manageConfigs}
            saving={alter.isPending}
            subject={`topic ${topic.name}`}
            onPatch={async (patch) => {
              await alter.mutateAsync(patch);
              notifySuccess('Topic configs updated', topic.name);
            }}
          />
        </Tabs.Panel>
        <Tabs.Panel value="messages" pt="md">
          {caps.browseMessages ? (
            <MessagesPanel clusterId={clusterId} topic={topic.name} partitionCount={partitionCount} />
          ) : (
            <TabNotAvailable what="Browsing records" />
          )}
        </Tabs.Panel>
        <Tabs.Panel value="produce" pt="md">
          {caps.browseMessages ? (
            <ProducePanel
              clusterId={clusterId}
              topic={topic.name}
              partitionCount={partitionCount}
              availability={produceAvailability}
            />
          ) : (
            <TabNotAvailable what="Producing records" />
          )}
        </Tabs.Panel>
      </Tabs>

      <Modal opened={adding} onClose={() => setAdding(false)} title={<Text fw={600}>Add partitions</Text>} centered>
        <form
          onSubmit={(e) => {
            e.preventDefault();
            void confirmAction({
              title: 'Add partitions',
              message: `Increase ${topic.name} from ${partitionCount} to ${newCount} partitions? Partitions can never be removed, and keys will map to different partitions afterwards.`,
              confirmLabel: 'Add partitions',
              danger: false,
            }).then((ok) => {
              if (!ok) return;
              addPartitions.mutate(
                { count: newCount },
                {
                  onSuccess: () => {
                    notifySuccess('Partitions added', `${topic.name} now has ${newCount} partitions`);
                    setAdding(false);
                  },
                },
              );
            });
          }}
        >
          <Stack>
            <NumberInput
              label="New total number of partitions"
              description={`Currently ${partitionCount}`}
              min={partitionCount + 1}
              value={newCount}
              onChange={(v) => setNewCount(Number(v) || partitionCount + 1)}
              data-autofocus
            />
            <Group justify="flex-end">
              <Button variant="default" onClick={() => setAdding(false)}>
                Cancel
              </Button>
              <Button type="submit" loading={addPartitions.isPending} disabled={newCount <= partitionCount}>
                Add partitions
              </Button>
            </Group>
          </Stack>
        </form>
      </Modal>
    </Stack>
  );
}
