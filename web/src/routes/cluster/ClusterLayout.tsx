// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { Alert, Badge, Button, Group, List, Stack, Tabs, Text } from '@mantine/core';
import {
  IconAlertTriangle,
  IconArrowBigUpLines,
  IconInfoCircle,
  IconLayoutDashboard,
  IconListDetails,
  IconRefresh,
  IconScale,
  IconServer,
  IconSettings,
  IconShieldLock,
  IconStack2,
  IconTopologyStar3,
  IconUsersGroup,
} from '@tabler/icons-react';
import { type ComponentType, useMemo } from 'react';
import { Outlet, useMatch, useNavigate, useParams } from 'react-router';

import { useCluster, useRefreshCluster } from '../../api/clusters';
import { isApiError } from '../../api/errors';
import { FlavorBadge, HealthBadge, ProviderBadge } from '../../components/badges';
import { CopyText } from '../../components/CopyText';
import { OperationPlannerProvider } from '../../components/operations/OperationPlanner';
import { PageHeader } from '../../components/PageHeader';
import { PageLoader, QueryError } from '../../components/QueryState';
import { RelativeTime } from '../../components/RelativeTime';
import { type ClusterTab, isClusterTab, visibleClusterTabs } from '../../lib/capabilities';
import { ClusterContext, type ClusterContextValue } from './clusterContext';

const TAB_META: Record<ClusterTab, { label: string; icon: ComponentType<{ size?: number }> }> = {
  overview: { label: 'Overview', icon: IconLayoutDashboard },
  brokers: { label: 'Brokers', icon: IconServer },
  controllers: { label: 'Controllers', icon: IconTopologyStar3 },
  topics: { label: 'Topics', icon: IconStack2 },
  distribution: { label: 'Distribution', icon: IconScale },
  groups: { label: 'Consumer groups', icon: IconUsersGroup },
  security: { label: 'Security', icon: IconShieldLock },
  operations: { label: 'Operations', icon: IconListDetails },
  upgrade: { label: 'Upgrade', icon: IconArrowBigUpLines },
  settings: { label: 'Settings', icon: IconSettings },
};

export default function ClusterLayout() {
  const { clusterId = '' } = useParams();
  const navigate = useNavigate();
  const { data: view, error, isPending, refetch } = useCluster(clusterId);
  const refresh = useRefreshCluster(clusterId);
  const tabParam = useMatch('/clusters/:clusterId/:tab/*')?.params.tab;
  const tab: ClusterTab = isClusterTab(tabParam) ? tabParam : 'overview';

  const ctx = useMemo<ClusterContextValue | null>(
    () =>
      view ? { clusterId, view, cluster: view.cluster, caps: view.capabilities, snapshot: view.snapshot } : null,
    [clusterId, view],
  );

  if (isPending) return <PageLoader label="Loading cluster" />;
  if (!ctx) {
    return (
      <QueryError
        error={error}
        title={isApiError(error, 404) ? 'Cluster not found' : 'Could not load the cluster'}
        onRetry={() => void refetch()}
      />
    );
  }

  const { cluster, caps, view: v } = ctx;
  const tabs = visibleClusterTabs(caps);
  const health = v.health;
  const tabBadge = (t: ClusterTab): number | null => {
    switch (t) {
      case 'brokers':
        return health.brokers;
      case 'topics':
        return health.topics;
      case 'groups':
        return v.snapshot?.groups.length ?? null;
      default:
        return null;
    }
  };

  return (
    <ClusterContext.Provider value={ctx}>
      <OperationPlannerProvider clusterId={clusterId} clusterName={cluster.name} capabilities={caps}>
        <PageHeader
          crumbs={[{ label: 'Clusters', to: '/' }, { label: cluster.name }]}
          title={cluster.name}
          badges={
            <Group gap={6}>
              <HealthBadge status={health.status} size="md" />
              <ProviderBadge provider={cluster.provider} size="md" />
              <FlavorBadge flavor={cluster.flavor} size="md" />
            </Group>
          }
          description={
            <Group gap="md" component="span">
              <Text span size="sm" c="dimmed">
                {cluster.bootstrapServers.join(', ')}
              </Text>
              {cluster.kafkaClusterId && (
                <Group gap={4} component="span">
                  <Text span size="sm" c="dimmed">
                    Cluster ID
                  </Text>
                  <CopyText value={cluster.kafkaClusterId} size="sm" c="dimmed" />
                </Group>
              )}
              <Text span size="sm" c="dimmed">
                Snapshot <RelativeTime value={v.snapshot?.collectedAt ?? v.updatedAt} size="sm" c="dimmed" />
              </Text>
            </Group>
          }
          actions={
            <Button
              variant="default"
              leftSection={<IconRefresh size={16} />}
              loading={refresh.isPending}
              onClick={() => refresh.mutate()}
            >
              Refresh
            </Button>
          }
        />

        <Stack gap="sm" mb="md">
          {v.error && (
            <Alert color="red" icon={<IconAlertTriangle size={18} />} title="The cluster is not reachable">
              {v.error}
            </Alert>
          )}
          {caps.notes.length > 0 && (
            <Alert color="blue" variant="light" icon={<IconInfoCircle size={18} />}>
              {caps.notes.length === 1 ? (
                caps.notes[0]
              ) : (
                <List size="sm" spacing={2}>
                  {caps.notes.map((note) => (
                    <List.Item key={note}>{note}</List.Item>
                  ))}
                </List>
              )}
            </Alert>
          )}
        </Stack>

        <Tabs
          value={tab}
          onChange={(value) => {
            if (!value) return;
            const base = `/clusters/${encodeURIComponent(clusterId)}`;
            navigate(value === 'overview' ? base : `${base}/${value}`);
          }}
          keepMounted={false}
          mb="md"
        >
          <Tabs.List style={{ flexWrap: 'nowrap', overflowX: 'auto', overflowY: 'hidden' }}>
            {tabs.map((t) => {
              const { label, icon: Icon } = TAB_META[t];
              const count = tabBadge(t);
              return (
                <Tabs.Tab
                  key={t}
                  value={t}
                  leftSection={<Icon size={16} />}
                  rightSection={
                    count !== null ? (
                      <Badge size="xs" variant="light" color="gray" circle={count < 10}>
                        {count}
                      </Badge>
                    ) : undefined
                  }
                  style={{ whiteSpace: 'nowrap' }}
                >
                  {label}
                </Tabs.Tab>
              );
            })}
          </Tabs.List>
        </Tabs>

        <Outlet />
      </OperationPlannerProvider>
    </ClusterContext.Provider>
  );
}
