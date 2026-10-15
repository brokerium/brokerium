// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { Alert, Group, Stack, Text } from '@mantine/core';
import { IconAlertTriangle, IconTrash } from '@tabler/icons-react';
import { useNavigate } from 'react-router';

import { useDeleteCluster, useUpdateCluster } from '../../api/clusters';
import { ClusterForm } from '../../components/clusters/ClusterForm';
import { confirmAction } from '../../components/confirm';
import { GatedButton } from '../../components/Gated';
import { notifySuccess } from '../../components/notify';
import { usePermissions } from '../../hooks/usePermissions';
import { clusterFormFromCluster } from '../../lib/clusterForm';
import { useClusterContext } from './clusterContext';
import { ReadOnlyNotice, Section } from './common';

export default function SettingsTab() {
  const { clusterId, cluster } = useClusterContext();
  const { canOperate, isAdmin } = usePermissions();
  const navigate = useNavigate();
  const update = useUpdateCluster(clusterId);
  const remove = useDeleteCluster();

  const deleteCluster = async () => {
    const ok = await confirmAction({
      title: `Delete cluster ${cluster.name}`,
      message: (
        <Stack gap="xs">
          <Text size="sm">
            Removes the registration, stored credentials and the operation history of this cluster from Brokerium.
          </Text>
          <Text size="sm">The Kafka cluster itself and its data are not touched.</Text>
        </Stack>
      ),
      confirmLabel: 'Delete cluster',
      typeToConfirm: cluster.name,
      typeToConfirmWhat: 'cluster name',
    });
    if (!ok) return;
    remove.mutate(clusterId, {
      onSuccess: () => {
        notifySuccess('Cluster deleted', cluster.name);
        navigate('/', { replace: true });
      },
    });
  };

  return (
    <Stack gap="md">
      {!canOperate && <ReadOnlyNotice />}
      <Section title="Connection" description="How Brokerium connects to the cluster over the Kafka protocol">
        <ClusterForm
          key={cluster.updatedAt}
          initial={clusterFormFromCluster(cluster)}
          mode="edit"
          readOnly={!canOperate}
          submitting={update.isPending}
          onSubmit={(request) => update.mutate(request, { onSuccess: () => notifySuccess('Cluster updated', cluster.name) })}
        />
      </Section>

      <Section title="Danger zone">
        <Alert color="red" variant="light" icon={<IconAlertTriangle size={18} />}>
          <Group justify="space-between" align="center">
            <Text size="sm">Delete the registration of this cluster from Brokerium. Only administrators can do this.</Text>
            <GatedButton
              availability={isAdmin ? { allowed: true } : { allowed: false, reason: 'Only administrators can delete clusters' }}
              color="red"
              leftSection={<IconTrash size={16} />}
              loading={remove.isPending}
              onClick={() => void deleteCluster()}
            >
              Delete cluster
            </GatedButton>
          </Group>
        </Alert>
      </Section>
    </Stack>
  );
}
