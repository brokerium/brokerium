// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { Modal } from '@mantine/core';
import { useNavigate } from 'react-router';

import { useCreateCluster } from '../../api/clusters';
import { EMPTY_CLUSTER_FORM } from '../../lib/clusterForm';
import { notifySuccess } from '../notify';
import { ClusterForm } from './ClusterForm';

interface AddClusterModalProps {
  opened: boolean;
  onClose: () => void;
}

export function AddClusterModal({ opened, onClose }: AddClusterModalProps) {
  const navigate = useNavigate();
  const create = useCreateCluster();
  return (
    <Modal opened={opened} onClose={onClose} title="Add cluster" size="xl" closeOnClickOutside={false}>
      <ClusterForm
        initial={EMPTY_CLUSTER_FORM}
        mode="create"
        submitting={create.isPending}
        onCancel={onClose}
        onSubmit={(request) =>
          create.mutate(request, {
            onSuccess: (view) => {
              notifySuccess('Cluster added', view.cluster.name);
              onClose();
              navigate(`/clusters/${encodeURIComponent(view.cluster.id)}`);
            },
          })
        }
      />
    </Modal>
  );
}
