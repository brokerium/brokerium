// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { Modal, SegmentedControl, Stack, useMatches } from '@mantine/core';
import { useState } from 'react';
import { useNavigate } from 'react-router';

import { useCreateCluster } from '../../api/clusters';
import { useServerInfo } from '../../api/server';
import { EMPTY_CLUSTER_FORM } from '../../lib/clusterForm';
import { notifySuccess } from '../notify';
import { ClusterForm } from './ClusterForm';
import { CreateAgentClusterForm } from './CreateAgentClusterForm';
import { CreateDockerClusterForm } from './CreateDockerClusterForm';

interface AddClusterModalProps {
  opened: boolean;
  onClose: () => void;
}

type Mode = 'connect' | 'agents' | 'docker';

export function AddClusterModal({ opened, onClose }: AddClusterModalProps) {
  const navigate = useNavigate();
  const create = useCreateCluster();
  const { data: server } = useServerInfo();
  const [selected, setSelected] = useState<Mode>('connect');
  const orientation = useMatches<'vertical' | 'horizontal'>({ base: 'vertical', sm: 'horizontal' });
  const dockerAvailable = Boolean(server?.providers.docker.available);
  const mode: Mode = selected === 'docker' && !dockerAvailable ? 'connect' : selected;
  const close = () => {
    setSelected('connect');
    onClose();
  };

  const body = () => {
    switch (mode) {
      case 'connect':
        return (
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
        );
      case 'agents':
        return <CreateAgentClusterForm onCancel={close} onDone={close} />;
      case 'docker':
        return <CreateDockerClusterForm onCancel={close} onDone={close} />;
      default: {
        const unhandled: never = mode;
        return unhandled;
      }
    }
  };

  return (
    <Modal
      opened={opened}
      onClose={close}
      title="Add cluster"
      size={mode === 'agents' ? '64rem' : 'xl'}
      closeOnClickOutside={false}
    >
      <Stack>
        <SegmentedControl
          value={mode}
          orientation={orientation}
          fullWidth
          onChange={(v) => setSelected(v as Mode)}
          data={[
            { value: 'connect', label: 'Connect an existing cluster' },
            { value: 'agents', label: 'Create on hosts with agents' },
            ...(dockerAvailable ? [{ value: 'docker', label: 'Create in Docker' }] : []),
          ]}
        />
        {body()}
      </Stack>
    </Modal>
  );
}
