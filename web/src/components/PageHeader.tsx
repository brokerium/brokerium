// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { Anchor, Breadcrumbs, Group, Stack, Text, Title } from '@mantine/core';
import type { ReactNode } from 'react';
import { Link } from 'react-router';

export interface Crumb {
  label: string;
  to?: string;
}

interface PageHeaderProps {
  title: ReactNode;
  description?: ReactNode;
  /** Badges or other inline elements next to the title. */
  badges?: ReactNode;
  actions?: ReactNode;
  crumbs?: Crumb[];
}

export function PageHeader({ title, description, badges, actions, crumbs }: PageHeaderProps) {
  return (
    <Stack gap={6} mb="md">
      {crumbs && crumbs.length > 0 && (
        <Breadcrumbs separatorMargin={6}>
          {crumbs.map((c) =>
            c.to ? (
              <Anchor key={c.label + c.to} component={Link} to={c.to} size="sm">
                {c.label}
              </Anchor>
            ) : (
              <Text key={c.label} size="sm" c="dimmed">
                {c.label}
              </Text>
            ),
          )}
        </Breadcrumbs>
      )}
      <Group justify="space-between" align="flex-start" gap="sm">
        <Stack gap={4} style={{ minWidth: 0 }}>
          <Group gap="sm" align="center">
            <Title order={2} style={{ wordBreak: 'break-all' }}>
              {title}
            </Title>
            {badges}
          </Group>
          {description && (
            <Text size="sm" c="dimmed">
              {description}
            </Text>
          )}
        </Stack>
        {actions && <Group gap="xs">{actions}</Group>}
      </Group>
    </Stack>
  );
}
