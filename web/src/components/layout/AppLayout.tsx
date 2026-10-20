// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import {
  AppShell,
  Box,
  Burger,
  Center,
  Group,
  NavLink,
  Progress,
  ScrollArea,
  Stack,
  Text,
  Tooltip,
  UnstyledButton,
} from '@mantine/core';
import { useDisclosure } from '@mantine/hooks';
import { IconHistory, IconLicense, IconListDetails, IconServer2, IconServerBolt, IconUsers } from '@tabler/icons-react';
import type { ComponentType } from 'react';
import { Link, Outlet, useLocation, useNavigation } from 'react-router';

import { useVersion } from '../../api/auth';
import { useOverview } from '../../api/clusters';
import { useLicense } from '../../api/license';
import type { HealthStatus, User } from '../../api/types';
import { licenseState, licenseStateColor, licenseStateLabel, needsAttention } from '../../lib/license';
import { healthColor, healthLabel } from '../../lib/status';
import { BrandMark } from './BrandMark';
import { ColorSchemeToggle } from './ColorSchemeToggle';
import { TrademarkNotice } from './TrademarkNotice';
import { UserMenu } from './UserMenu';

interface NavItem {
  to: string;
  label: string;
  icon: ComponentType<{ size?: number; stroke?: number }>;
  active: (pathname: string) => boolean;
  adminOnly?: boolean;
}

const NAV_ITEMS: NavItem[] = [
  { to: '/', label: 'Clusters', icon: IconServer2, active: (p) => p === '/' || p.startsWith('/clusters') },
  { to: '/operations', label: 'Operations', icon: IconListDetails, active: (p) => p.startsWith('/operations') },
  { to: '/nodes', label: 'Nodes', icon: IconServerBolt, active: (p) => p.startsWith('/nodes') },
  { to: '/users', label: 'Users', icon: IconUsers, active: (p) => p.startsWith('/users'), adminOnly: true },
  { to: '/audit', label: 'Audit', icon: IconHistory, active: (p) => p.startsWith('/audit') },
  { to: '/license', label: 'License', icon: IconLicense, active: (p) => p.startsWith('/license') },
];

function Dot({ color, label }: { color: string; label: string }) {
  return (
    <Tooltip label={label} withArrow position="right">
      <Box w={8} h={8} style={{ borderRadius: '50%', background: `var(--mantine-color-${color}-6)` }} />
    </Tooltip>
  );
}

function HealthDot({ status }: { status: HealthStatus }) {
  return <Dot color={healthColor(status)} label={healthLabel(status)} />;
}

function NavigationProgress() {
  const navigation = useNavigation();
  if (navigation.state === 'idle') return null;
  return <Progress value={100} animated size={3} radius={0} pos="fixed" top={0} left={0} right={0} style={{ zIndex: 1000 }} />;
}

export function AppLayout({ user }: { user: User }) {
  const [opened, { toggle, close }] = useDisclosure();
  const { pathname } = useLocation();
  const { data: clusters } = useOverview();
  const { data: version } = useVersion();
  const { data: license } = useLicense();
  const state = licenseState(license?.license);
  const licenseDot = needsAttention(license) ? (
    <Dot color={licenseStateColor(state)} label={`License: ${licenseStateLabel(state)}`} />
  ) : undefined;

  return (
    <AppShell
      header={{ height: 60 }}
      navbar={{ width: 250, breakpoint: 'sm', collapsed: { mobile: !opened } }}
      footer={{ height: { base: 52, sm: 32 } }}
      padding="md"
    >
      <NavigationProgress />
      <AppShell.Header>
        <Group h="100%" px="md" justify="space-between" wrap="nowrap">
          <Group gap="sm" wrap="nowrap">
            <Burger opened={opened} onClick={toggle} hiddenFrom="sm" size="sm" aria-label="Toggle navigation" />
            <UnstyledButton component={Link} to="/" aria-label="Brokerium home">
              <BrandMark />
            </UnstyledButton>
          </Group>
          <Group gap="sm" wrap="nowrap">
            <ColorSchemeToggle />
            <UserMenu user={user} />
          </Group>
        </Group>
      </AppShell.Header>

      <AppShell.Navbar p="xs">
        <AppShell.Section grow component={ScrollArea}>
          {NAV_ITEMS.filter((item) => !item.adminOnly || user.role === 'admin').map((item) => (
            <Box key={item.to}>
              <NavLink
                component={Link}
                to={item.to}
                label={item.label}
                leftSection={<item.icon size={18} stroke={1.6} />}
                rightSection={item.to === '/license' ? licenseDot : undefined}
                active={item.active(pathname)}
                onClick={close}
                fw={500}
              />
              {item.to === '/' && clusters && clusters.length > 0 && (
                <Stack gap={0} pl="lg" mb={4}>
                  {clusters.map(({ cluster, health }) => {
                    const to = `/clusters/${encodeURIComponent(cluster.id)}`;
                    return (
                      <NavLink
                        key={cluster.id}
                        component={Link}
                        to={to}
                        label={cluster.name}
                        leftSection={<HealthDot status={health.status} />}
                        active={pathname === to || pathname.startsWith(`${to}/`)}
                        variant="subtle"
                        onClick={close}
                        py={4}
                      />
                    );
                  })}
                </Stack>
              )}
            </Box>
          ))}
        </AppShell.Section>
        <AppShell.Section>
          <Text size="xs" c="dimmed" px="sm" py={4}>
            Brokerium {license?.edition === 'enterprise' ? 'Enterprise ' : ''}
            {version?.version ?? ''}
            {version?.commit ? ` · ${version.commit.slice(0, 7)}` : ''}
          </Text>
        </AppShell.Section>
      </AppShell.Navbar>

      <AppShell.Main>
        <Box maw={1600} mx="auto">
          <Outlet />
        </Box>
      </AppShell.Main>

      <AppShell.Footer>
        <Center h="100%" px="md">
          <TrademarkNotice />
        </Center>
      </AppShell.Footer>
    </AppShell>
  );
}
