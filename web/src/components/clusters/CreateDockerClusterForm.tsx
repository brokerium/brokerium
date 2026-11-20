// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { Alert, Button, Group, NumberInput, Stack, Text, TextInput } from '@mantine/core';
import { IconAlertTriangle } from '@tabler/icons-react';
import { useState } from 'react';

import { type PlannedCluster, useCreateNewCluster } from '../../api/clusters';
import { COMBINED_WARNING, MAX_VOTERS } from '../../lib/bootstrap';
import { CLUSTER_NAME_PATTERN } from '../../lib/clusterForm';
import { KAFKA_20979_WARNING, affectedByKafka20979, newClusterVersionError } from '../../lib/kafkaVersion';
import { quorumHint } from '../../lib/quorum';
import { PlannedClusterPreview, type Quorum, QuorumField } from './NewClusterParts';

interface CreateDockerClusterFormProps {
  onCancel: () => void;
  onDone: () => void;
}

/**
 * Creates a cluster whose nodes run as containers on the server's Docker host: registers the
 * cluster, shows the bootstrap-cluster plan and starts it after confirmation.
 */
export function CreateDockerClusterForm({ onCancel, onDone }: CreateDockerClusterFormProps) {
  const create = useCreateNewCluster();
  const [name, setName] = useState('');
  const [version, setVersion] = useState('4.3.1');
  const [controllers, setControllers] = useState(3);
  const [combined, setCombined] = useState(0);
  const [brokers, setBrokers] = useState(3);
  const [quorum, setQuorum] = useState<Quorum>('dynamic');
  const [racks, setRacks] = useState('rack-a, rack-b, rack-c');
  const [planned, setPlanned] = useState<PlannedCluster | null>(null);

  const nameError = name !== '' && !CLUSTER_NAME_PATTERN.test(name) ? 'Lowercase letters, digits and dashes' : undefined;
  const versionError = newClusterVersionError(version);
  const voters = controllers + combined;
  const countError =
    voters < 1 || voters > MAX_VOTERS
      ? `Controllers and combined nodes together: 1 to ${MAX_VOTERS}`
      : brokers + combined < 1
        ? 'At least one broker or combined node'
        : undefined;
  const rackList = racks
    .split(',')
    .map((r) => r.trim())
    .filter(Boolean);
  const valid = name !== '' && !nameError && !versionError && !countError;

  const submit = () =>
    create.mutate(
      {
        name,
        provider: 'docker',
        params: {
          controllers,
          brokers,
          ...(combined > 0 ? { combined } : {}),
          version: version.trim(),
          quorum,
          racks: rackList,
        },
      },
      { onSuccess: setPlanned },
    );

  if (planned) return <PlannedClusterPreview planned={planned} onBack={() => setPlanned(null)} onDone={onDone} />;

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
        <NumberInput label="Controllers" min={0} max={MAX_VOTERS} value={controllers} onChange={(v) => setControllers(Number(v) || 0)} />
        <NumberInput
          label="Combined"
          description="Broker and controller"
          min={0}
          max={MAX_VOTERS}
          value={combined}
          onChange={(v) => setCombined(Number(v) || 0)}
        />
        <NumberInput label="Brokers" min={0} max={9} value={brokers} onChange={(v) => setBrokers(Number(v) || 0)} />
      </Group>
      <Text size="xs" c={countError ? 'red' : 'dimmed'}>
        {countError ?? quorumHint(voters)}
      </Text>
      <QuorumField value={quorum} onChange={setQuorum} />
      <TextInput
        label="Racks"
        description="Comma-separated; brokers are spread over them in turn"
        value={racks}
        onChange={(e) => setRacks(e.currentTarget.value)}
      />
      {combined > 0 && (
        <Alert color="yellow" variant="light" icon={<IconAlertTriangle size={18} />}>
          {COMBINED_WARNING}
        </Alert>
      )}
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
