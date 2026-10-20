// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { Button, Group, SegmentedControl, Stack, Text } from '@mantine/core';
import { IconPlayerPlay } from '@tabler/icons-react';
import { useNavigate } from 'react-router';

import { type PlannedCluster, useDeleteCluster } from '../../api/clusters';
import { useConfirmOperation, useDiscardOperation } from '../../api/operations';
import { notifySuccess } from '../notify';
import { PlanPreview } from '../operations/PlanPreview';

export type Quorum = 'dynamic' | 'static';

export function QuorumField({ value, onChange }: { value: Quorum; onChange: (value: Quorum) => void }) {
  return (
    <Stack gap={4}>
      <Text size="sm" fw={500}>
        Controller quorum
      </Text>
      <SegmentedControl
        value={value}
        onChange={(v) => onChange(v as Quorum)}
        data={[
          { value: 'dynamic', label: 'Dynamic (KIP-853)' },
          { value: 'static', label: 'Static' },
        ]}
      />
    </Stack>
  );
}

interface PlannedClusterPreviewProps {
  planned: PlannedCluster;
  /** Back to the form; the planned operation and the empty registration are removed. */
  onBack: () => void;
  onDone: () => void;
}

/** The bootstrap-cluster plan of a new cluster with Back and Create cluster. */
export function PlannedClusterPreview({ planned, onBack, onDone }: PlannedClusterPreviewProps) {
  const navigate = useNavigate();
  const confirm = useConfirmOperation();
  const discard = useDiscardOperation();
  const remove = useDeleteCluster();

  const back = () => {
    discard.mutate({ id: planned.operation.id, clusterId: planned.view.cluster.id });
    remove.mutate(planned.view.cluster.id);
    onBack();
  };

  const start = () =>
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
