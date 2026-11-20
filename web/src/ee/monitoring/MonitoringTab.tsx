// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

import { Alert, Badge, Button, Code, Group, Paper, SegmentedControl, SimpleGrid, Stack, Text, Tooltip } from '@mantine/core';
import { IconAlertTriangle, IconChartLine, IconFilter } from '@tabler/icons-react';
import { useState } from 'react';
import { Link } from 'react-router';

import { useLicense } from '../../api/license';
import { EnterpriseBadge } from '../../components/badges';
import { PageLoader, QueryError } from '../../components/QueryState';
import { useFeature } from '../../hooks/useFeature';
import { usePermissions } from '../../hooks/usePermissions';
import { formatNumber } from '../../lib/format';
import { useClusterContext } from '../../routes/cluster/clusterContext';
import {
  type ClusterMonitoring,
  type MonitoringProfile,
  type MonitoringRange,
  type MonitoringSection,
  useClusterMonitoring,
} from '../api/monitoring';
import { MONITORING } from '../features';
import { RANGES, SECTIONS } from './chart';
import { CurrentValues } from './CurrentValues';
import { PanelCard } from './PanelCard';
import { SelectorModal } from './SelectorModal';

function ProfileBadges({ profile }: { profile: MonitoringProfile }) {
  return (
    <Group gap={6}>
      {profile.exporter && (
        <Tooltip label={`JMX exporter ${profile.exporter}: ${profile.heapMetric}`} withArrow>
          <Badge variant="light" color="gray">
            Exporter {profile.exporter}
          </Badge>
        </Tooltip>
      )}
      {profile.kraft && (
        <Badge variant="light" color="gray">
          KRaft metrics
        </Badge>
      )}
      <Badge variant="light" color={profile.brokers > 0 ? 'gray' : 'yellow'}>
        {formatNumber(profile.brokers)} {profile.brokers === 1 ? 'broker' : 'brokers'} reporting
      </Badge>
    </Group>
  );
}

/** Tells why charts are missing: no data source, an unreachable Prometheus or no matching exporter. */
function Notices({ monitoring, isAdmin }: { monitoring: ClusterMonitoring; isAdmin: boolean }) {
  const { source, profile, selector } = monitoring;
  if (!source.configured) {
    return (
      <Alert color="blue" icon={<IconChartLine size={18} />} title="Connect Prometheus to chart this cluster">
        <Stack gap="xs" align="flex-start">
          <Text size="sm">
            The Monitoring tab charts the broker, topic and JVM metrics that your Prometheus collects from the JMX exporters of
            the brokers. Until a data source is set, it shows the values Brokerium samples now.
          </Text>
          {isAdmin ? (
            <Button component={Link} to="/monitoring" size="xs" variant="light">
              Configure Prometheus
            </Button>
          ) : (
            <Text size="sm" c="dimmed">
              An administrator sets the data source on the Monitoring page.
            </Text>
          )}
        </Stack>
      </Alert>
    );
  }
  if (source.error) {
    return (
      <Alert color="red" icon={<IconAlertTriangle size={18} />} title="The Prometheus data source cannot be used">
        {source.error}
      </Alert>
    );
  }
  if (!selector.effective) {
    return (
      <Alert color="yellow" icon={<IconAlertTriangle size={18} />} title="No selector">
        Brokerium does not know the addresses of the brokers yet. {isAdmin ? 'Set a selector for this cluster.' : ''}
      </Alert>
    );
  }
  if (profile?.error) {
    return (
      <Alert color="red" icon={<IconAlertTriangle size={18} />} title="Prometheus did not answer">
        {profile.error}
      </Alert>
    );
  }
  if (profile && profile.brokers === 0 && !profile.exporter) {
    return (
      <Alert color="yellow" icon={<IconAlertTriangle size={18} />} title="No exporter metrics match the selector">
        Check that the JMX exporter runs on the brokers and that Prometheus scrapes it
        {selector.custom ? '' : `, or ${isAdmin ? 'set' : 'ask an administrator to set'} a selector that matches its targets`}.
      </Alert>
    );
  }
  return null;
}

export default function MonitoringTab() {
  const { clusterId, cluster, snapshot } = useClusterContext();
  const license = useLicense();
  const { availability } = useFeature(MONITORING);
  const { isAdmin } = usePermissions();
  const monitoring = useClusterMonitoring(clusterId, availability.allowed);
  const [range, setRange] = useState<MonitoringRange>('1h');
  const [section, setSection] = useState<MonitoringSection>('cluster');
  const [editing, setEditing] = useState(false);

  if (license.isPending) return <PageLoader label="Loading monitoring" />;
  if (!availability.allowed) {
    return (
      <Stack gap="md">
        <Alert
          title={
            <Group gap="xs">
              Monitoring <EnterpriseBadge size="xs" />
            </Group>
          }
        >
          <Text size="sm">
            {availability.reason}. See <Link to="/license">License</Link> for the features of Brokerium Enterprise.
          </Text>
        </Alert>
        <CurrentValues clusterId={clusterId} snapshot={snapshot} />
      </Stack>
    );
  }
  if (monitoring.isPending) return <PageLoader label="Loading monitoring" />;
  if (!monitoring.data) {
    return <QueryError error={monitoring.error} title="Could not load the monitoring" onRetry={() => void monitoring.refetch()} />;
  }

  const m = monitoring.data;
  const { profile, selector } = m;
  const charts = m.source.configured && !m.source.error && selector.effective !== '';
  const nothingMatches = profile !== null && !profile.error && profile.brokers === 0 && !profile.exporter;
  const panels = m.panels.filter((p) => p.section === section);

  return (
    <Stack gap="md">
      <Notices monitoring={m} isAdmin={isAdmin} />
      {charts && (
        <Paper withBorder radius="md" p="sm">
          <Group justify="space-between" gap="sm">
            <Group gap="sm">
              <SegmentedControl data={SECTIONS} value={section} onChange={setSection} size="xs" />
              <SegmentedControl data={RANGES} value={range} onChange={setRange} size="xs" />
            </Group>
            <Group gap="xs">
              {profile && !profile.error && <ProfileBadges profile={profile} />}
              {isAdmin && (
                <Button size="xs" variant="default" leftSection={<IconFilter size={14} />} onClick={() => setEditing(true)}>
                  Selector
                </Button>
              )}
            </Group>
          </Group>
          <Text size="xs" c="dimmed" mt="xs" style={{ wordBreak: 'break-all' }}>
            {selector.custom ? 'Custom selector' : 'Default selector'} <Code>{selector.effective}</Code>
          </Text>
        </Paper>
      )}
      {!charts && isAdmin && m.source.configured && (
        <Group>
          <Button size="xs" variant="default" leftSection={<IconFilter size={14} />} onClick={() => setEditing(true)}>
            Selector
          </Button>
        </Group>
      )}
      {charts && (
        <SimpleGrid cols={{ base: 1, md: 2, xl: 3 }} spacing="md">
          {panels.map((p) => (
            <PanelCard key={`${p.id}-${selector.effective}`} clusterId={clusterId} panel={p} range={range} />
          ))}
        </SimpleGrid>
      )}
      {(!charts || nothingMatches) && <CurrentValues clusterId={clusterId} snapshot={snapshot} />}
      {editing && (
        <SelectorModal clusterId={clusterId} clusterName={cluster.name} selector={selector} onClose={() => setEditing(false)} />
      )}
    </Stack>
  );
}
