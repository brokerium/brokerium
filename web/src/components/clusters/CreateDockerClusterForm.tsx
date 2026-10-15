// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { Alert, Button, Group, NumberInput, SegmentedControl, Stack, Text, TextInput } from '@mantine/core';
import { IconAlertTriangle, IconPlayerPlay } from '@tabler/icons-react';
import { useState } from 'react';
import { useNavigate } from 'react-router';

import { type PlannedDockerCluster, useCreateDockerCluster, useDeleteCluster } from '../../api/clusters';
import { useConfirmOperation, useDiscardOperation } from '../../api/operations';
import { CLUSTER_NAME_PATTERN } from '../../lib/clusterForm';
import { KAFKA_20979_WARNING, affectedByKafka20979, parseKafkaVersion } from '../../lib/kafkaVersion';
import { quorumHint } from '../../lib/quorum';
import { notifySuccess } from '../notify';
import { PlanPreview } from '../operations/PlanPreview';

interface CreateDockerClusterFormProps {
  onCancel: () => void;
  onDone: () => void;
}

type Quorum = 'dynamic' | 'static';

/**
 * Creates a cluster whose nodes run as containers on the server's Docker host: registers the
 * cluster, shows the bootstrap-cluster plan and starts it after confirmation.
 */
export function CreateDockerClusterForm({ onCancel, onDone }: CreateDockerClusterFormProps) {
  const navigate = useNavigate();
  const create = useCreateDockerCluster();
  const confirm = useConfirmOperation();
  const discard = useDiscardOperation();
  const remove = useDeleteCluster();
  const [name, setName] = useState('');
  const [version, setVersion] = useState('4.3.1');
  const [controllers, setControllers] = useState(3);
  const [brokers, setBrokers] = useState(3);
  const [quorum, setQuorum] = useState<Quorum>('dynamic');
  const [racks, setRacks] = useState('rack-a, rack-b, rack-c');
  const [planned, setPlanned] = useState<PlannedDockerCluster | null>(null);

  const nameError = name !== '' && !CLUSTER_NAME_PATTERN.test(name) ? 'Lowercase letters, digits and dashes' : undefined;
  const versionError = parseKafkaVersion(version) ? undefined : 'A release such as 4.3.1';
  const rackList = racks
    .split(',')
    .map((r) => r.trim())
    .filter(Boolean);
  const valid = name !== '' && !nameError && !versionError;

  const submit = () =>
    create.mutate(
      { name, params: { controllers, brokers, version: version.trim(), quorum, racks: rackList } },
      { onSuccess: setPlanned },
    );

  const back = () => {
    if (!planned) return;
    discard.mutate({ id: planned.operation.id, clusterId: planned.view.cluster.id });
    remove.mutate(planned.view.cluster.id);
    setPlanned(null);
  };

  const start = () => {
    if (!planned) return;
    confirm.mutate(
      { id: planned.operation.id },
      {
        onSuccess: (op) => {
          notifySuccess('Creating cluster', planned.view.cluster.name);
          onDone();
          navigate(`/operations/${encodeURIComponent(op.id)}`);
        },
      },
    );
  };

  if (planned) {
    return (
      <Stack>
        <Text size="sm" c="dimmed">
          Dry run for the new cluster <b>{planned.view.cluster.name}</b>. Nothing is created until you start it.
        </Text>
        <PlanPreview plan={planned.operation.plan} irreversible={false} />
        <Group justify="space-between">
          <Button variant="default" onClick={back} loading={discard.isPending || remove.isPending}>
            Back
          </Button>
          <Button leftSection={<IconPlayerPlay size={16} />} onClick={start} loading={confirm.isPending}>
            Create cluster
          </Button>
        </Group>
      </Stack>
    );
  }

  return (
    <Stack>
      <Text size="sm" c="dimmed">
        Brokerium starts every controller and broker as a container with brokerium-agent on the Docker host of this
        server. Use it for labs and tests; production nodes run the agent on their own hosts.
      </Text>
      <TextInput
        label="Name"
        required
        value={name}
        onChange={(e) => setName(e.currentTarget.value)}
        error={nameError}
        data-autofocus
      />
      <Group grow align="flex-start">
        <TextInput label="Kafka version" value={version} onChange={(e) => setVersion(e.currentTarget.value)} error={versionError} />
        <NumberInput label="Controllers" min={1} max={5} value={controllers} onChange={(v) => setControllers(Number(v) || 1)} />
        <NumberInput label="Brokers" min={1} max={9} value={brokers} onChange={(v) => setBrokers(Number(v) || 1)} />
      </Group>
      <Text size="xs" c="dimmed">
        {quorumHint(controllers)}
      </Text>
      <Stack gap={4}>
        <Text size="sm" fw={500}>
          Controller quorum
        </Text>
        <SegmentedControl
          value={quorum}
          onChange={(v) => setQuorum(v as Quorum)}
          data={[
            { value: 'dynamic', label: 'Dynamic (KIP-853)' },
            { value: 'static', label: 'Static' },
          ]}
        />
      </Stack>
      <TextInput
        label="Racks"
        description="Comma-separated; brokers are spread over them in turn"
        value={racks}
        onChange={(e) => setRacks(e.currentTarget.value)}
      />
      {affectedByKafka20979(version) && (
        <Alert color="yellow" variant="light" icon={<IconAlertTriangle size={18} />}>
          {KAFKA_20979_WARNING}
        </Alert>
      )}
      <Group justify="flex-end">
        <Button variant="default" onClick={onCancel}>
          Cancel
        </Button>
        <Button disabled={!valid} loading={create.isPending} onClick={submit}>
          Preview plan
        </Button>
      </Group>
    </Stack>
  );
}
