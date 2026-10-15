// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { Modal, SegmentedControl, Stack } from '@mantine/core';
import { useState } from 'react';
import { useNavigate } from 'react-router';

import { useCreateCluster } from '../../api/clusters';
import { useOperationTypes } from '../../api/operations';
import { EMPTY_CLUSTER_FORM } from '../../lib/clusterForm';
import { notifySuccess } from '../notify';
import { ClusterForm } from './ClusterForm';
import { CreateDockerClusterForm } from './CreateDockerClusterForm';

interface AddClusterModalProps {
  opened: boolean;
  onClose: () => void;
}

type Mode = 'connect' | 'docker';

export function AddClusterModal({ opened, onClose }: AddClusterModalProps) {
  const navigate = useNavigate();
  const create = useCreateCluster();
  const { data: types } = useOperationTypes();
  const [mode, setMode] = useState<Mode>('connect');
  const canCreate = Boolean(types?.some((t) => t.type === 'bootstrap-cluster'));
  const close = () => {
    setMode('connect');
    onClose();
  };

  return (
    <Modal opened={opened} onClose={close} title="Add cluster" size="xl" closeOnClickOutside={false}>
      <Stack>
        {canCreate && (
          <SegmentedControl
            value={mode}
            onChange={(v) => setMode(v as Mode)}
            data={[
              { value: 'connect', label: 'Connect an existing cluster' },
              { value: 'docker', label: 'Create a cluster in Docker' },
            ]}
          />
        )}
        {mode === 'docker' && canCreate ? (
          <CreateDockerClusterForm onCancel={close} onDone={close} />
        ) : (
          <ClusterForm
            initial={EMPTY_CLUSTER_FORM}
            mode="create"
            submitting={create.isPending}
            onCancel={close}
            onSubmit={(request) =>
              create.mutate(request, {
                onSuccess: (view) => {
                  notifySuccess('Cluster added', view.cluster.name);
                  close();
                  navigate(`/clusters/${encodeURIComponent(view.cluster.id)}`);
                },
              })
            }
          />
        )}
      </Stack>
    </Modal>
  );
}
