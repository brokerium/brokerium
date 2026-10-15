// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { Drawer, Stack, Text } from '@mantine/core';

import { useAlterBrokerConfigs, useBrokerConfigs } from '../../api/clusters';
import { ConfigTable } from '../../components/ConfigTable';
import { notifySuccess } from '../../components/notify';
import { PageLoader, QueryError } from '../../components/QueryState';
import { usePermissions } from '../../hooks/usePermissions';
import { capabilityAvailability, withRole } from '../../lib/capabilities';
import { useClusterContext } from './clusterContext';

interface BrokerConfigsDrawerProps {
  brokerId: number | null;
  onClose: () => void;
}

/** Dynamic configs of one broker; per-broker overrides can be added, changed and removed. */
export function BrokerConfigsDrawer({ brokerId, onClose }: BrokerConfigsDrawerProps) {
  const { clusterId, caps } = useClusterContext();
  const { user } = usePermissions();
  const configs = useBrokerConfigs(clusterId, brokerId);
  const alter = useAlterBrokerConfigs(clusterId, brokerId ?? -1);
  const editable = withRole(capabilityAvailability(caps, 'manageConfigs'), user?.role);

  return (
    <Drawer
      opened={brokerId !== null}
      onClose={onClose}
      position="right"
      size="xl"
      title={<Text fw={700}>Broker {brokerId} configs</Text>}
    >
      {brokerId !== null && (
        <Stack>
          <Text size="sm" c="dimmed">
            Overrides set here are dynamic per-broker configs (source &quot;dynamic broker&quot;) and take effect without
            a restart when Kafka supports it for the key.
          </Text>
          {configs.isPending && <PageLoader />}
          {configs.error && <QueryError error={configs.error} onRetry={() => void configs.refetch()} />}
          {configs.data && (
            <ConfigTable
              configs={configs.data.configs}
              overrideSource="DYNAMIC_BROKER_CONFIG"
              editable={editable}
              saving={alter.isPending}
              subject={`broker ${brokerId}`}
              onPatch={async (patch) => {
                await alter.mutateAsync(patch);
                notifySuccess('Broker configs updated', `Broker ${brokerId}`);
              }}
            />
          )}
        </Stack>
      )}
    </Drawer>
  );
}
