// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { Paper, Tabs } from '@mantine/core';
import { IconGauge, IconKey, IconLock } from '@tabler/icons-react';
import { useState } from 'react';

import { usePermissions } from '../../hooks/usePermissions';
import { capabilityAvailability, type SecuritySection, securitySections, withRole } from '../../lib/capabilities';
import { useClusterContext } from './clusterContext';
import { TabNotAvailable } from './common';
import { AclsPanel } from './security/AclsPanel';
import { QuotasPanel } from './security/QuotasPanel';
import { ScramPanel } from './security/ScramPanel';

export default function SecurityTab() {
  const { clusterId, caps } = useClusterContext();
  const { user } = usePermissions();
  const sections = securitySections(caps);
  const [active, setActive] = useState<SecuritySection | null>(sections[0] ?? null);

  if (sections.length === 0) return <TabNotAvailable what="Security management" />;
  const current = active && sections.includes(active) ? active : sections[0];

  return (
    <Paper withBorder radius="md" p="md">
      <Tabs value={current} onChange={(v) => setActive(v as SecuritySection)} keepMounted={false} variant="pills">
        <Tabs.List mb="md">
          {sections.includes('acls') && (
            <Tabs.Tab value="acls" leftSection={<IconLock size={16} />}>
              ACLs
            </Tabs.Tab>
          )}
          {sections.includes('scram') && (
            <Tabs.Tab value="scram" leftSection={<IconKey size={16} />}>
              SCRAM users
            </Tabs.Tab>
          )}
          {sections.includes('quotas') && (
            <Tabs.Tab value="quotas" leftSection={<IconGauge size={16} />}>
              Quotas
            </Tabs.Tab>
          )}
        </Tabs.List>
        <Tabs.Panel value="acls">
          <AclsPanel clusterId={clusterId} editable={withRole(capabilityAvailability(caps, 'manageAcls'), user?.role)} />
        </Tabs.Panel>
        <Tabs.Panel value="scram">
          <ScramPanel clusterId={clusterId} editable={withRole(capabilityAvailability(caps, 'manageScram'), user?.role)} />
        </Tabs.Panel>
        <Tabs.Panel value="quotas">
          <QuotasPanel clusterId={clusterId} editable={withRole(capabilityAvailability(caps, 'manageQuotas'), user?.role)} />
        </Tabs.Panel>
      </Tabs>
    </Paper>
  );
}
