// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { Avatar, Group, Menu, Text, UnstyledButton } from '@mantine/core';
import { IconChevronDown, IconLogout, IconUserShield } from '@tabler/icons-react';
import { useNavigate } from 'react-router';

import { useLogout } from '../../api/auth';
import type { User } from '../../api/types';
import { ROLE_LABELS } from '../../lib/labels';

export function UserMenu({ user }: { user: User }) {
  const navigate = useNavigate();
  const logout = useLogout();
  const signOut = () =>
    logout.mutate(undefined, {
      onSettled: () => navigate('/login', { replace: true }),
    });
  return (
    <Menu position="bottom-end" width={220} withArrow>
      <Menu.Target>
        <UnstyledButton aria-label="User menu">
          <Group gap={8} wrap="nowrap">
            <Avatar size="sm" radius="xl" color="indigo">
              {user.username.slice(0, 2).toUpperCase()}
            </Avatar>
            <Text size="sm" fw={500} visibleFrom="sm">
              {user.username}
            </Text>
            <IconChevronDown size={14} />
          </Group>
        </UnstyledButton>
      </Menu.Target>
      <Menu.Dropdown>
        <Menu.Label>Signed in as {user.username}</Menu.Label>
        <Menu.Item leftSection={<IconUserShield size={16} />} disabled>
          Role: {ROLE_LABELS[user.role]}
        </Menu.Item>
        <Menu.Divider />
        <Menu.Item color="red" leftSection={<IconLogout size={16} />} onClick={signOut}>
          Log out
        </Menu.Item>
      </Menu.Dropdown>
    </Menu>
  );
}
