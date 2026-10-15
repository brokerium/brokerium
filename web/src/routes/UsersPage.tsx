// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import {
  ActionIcon,
  Alert,
  Badge,
  Button,
  Group,
  Menu,
  Modal,
  Paper,
  PasswordInput,
  Select,
  Stack,
  Switch,
  Table,
  Text,
  TextInput,
  Tooltip,
} from '@mantine/core';
import { IconDotsVertical, IconKey, IconLock, IconPlus, IconTrash } from '@tabler/icons-react';
import { useState } from 'react';

import type { Role, User } from '../api/types';
import { useCreateUser, useDeleteUser, useUpdateUser, useUsers } from '../api/users';
import { confirmAction } from '../components/confirm';
import { notifySuccess } from '../components/notify';
import { PageHeader } from '../components/PageHeader';
import { PageLoader, QueryError } from '../components/QueryState';
import { RelativeTime } from '../components/RelativeTime';
import { usePermissions } from '../hooks/usePermissions';
import { ROLE_LABELS, selectData } from '../lib/labels';

const ROLE_DESCRIPTIONS: Record<Role, string> = {
  admin: 'Everything, including users and deleting clusters',
  operator: 'Register clusters, change data and run operations',
  viewer: 'Read-only access',
};

function CreateUserModal({ onClose }: { onClose: () => void }) {
  const create = useCreateUser();
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [role, setRole] = useState<Role>('viewer');
  return (
    <Modal opened onClose={onClose} title={<Text fw={600}>Create user</Text>} centered>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          create.mutate(
            { username: username.trim(), password, role },
            {
              onSuccess: () => {
                notifySuccess('User created', username.trim());
                onClose();
              },
            },
          );
        }}
      >
        <Stack>
          <TextInput label="Username" required value={username} onChange={(e) => setUsername(e.currentTarget.value)} data-autofocus autoComplete="off" />
          <PasswordInput label="Password" required value={password} onChange={(e) => setPassword(e.currentTarget.value)} autoComplete="new-password" />
          <Select
            label="Role"
            description={ROLE_DESCRIPTIONS[role]}
            data={selectData(ROLE_LABELS)}
            value={role}
            onChange={(v) => v && setRole(v as Role)}
            allowDeselect={false}
          />
          <Group justify="flex-end">
            <Button variant="default" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" loading={create.isPending} disabled={!username.trim() || !password}>
              Create user
            </Button>
          </Group>
        </Stack>
      </form>
    </Modal>
  );
}

function ResetPasswordModal({ user, onClose }: { user: User; onClose: () => void }) {
  const update = useUpdateUser();
  const [password, setPassword] = useState('');
  return (
    <Modal opened onClose={onClose} title={<Text fw={600}>Reset password of {user.username}</Text>} centered>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          update.mutate(
            { id: user.id, password },
            {
              onSuccess: () => {
                notifySuccess('Password changed', user.username);
                onClose();
              },
            },
          );
        }}
      >
        <Stack>
          <PasswordInput label="New password" value={password} onChange={(e) => setPassword(e.currentTarget.value)} autoComplete="new-password" data-autofocus />
          <Group justify="flex-end">
            <Button variant="default" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" loading={update.isPending} disabled={!password}>
              Change password
            </Button>
          </Group>
        </Stack>
      </form>
    </Modal>
  );
}

export default function UsersPage() {
  const { user: me, isAdmin } = usePermissions();
  const users = useUsers(isAdmin);
  const update = useUpdateUser();
  const remove = useDeleteUser();
  const [creating, setCreating] = useState(false);
  const [resetting, setResetting] = useState<User | null>(null);

  if (!isAdmin) {
    return (
      <>
        <PageHeader title="Users" />
        <Alert color="gray" icon={<IconLock size={18} />}>
          Only administrators can manage users.
        </Alert>
      </>
    );
  }

  const changeRole = async (u: User, role: Role) => {
    if (role === u.role) return;
    const ok = await confirmAction({
      title: 'Change role',
      message: `Change the role of ${u.username} from ${ROLE_LABELS[u.role]} to ${ROLE_LABELS[role]}?`,
      confirmLabel: 'Change role',
      danger: role === 'admin',
    });
    if (ok) update.mutate({ id: u.id, role }, { onSuccess: () => notifySuccess('Role changed', u.username) });
  };

  const toggleDisabled = async (u: User) => {
    if (!u.disabled) {
      const ok = await confirmAction({
        title: `Disable ${u.username}`,
        message: 'The user can no longer sign in. Existing sessions stop working on the next request.',
        confirmLabel: 'Disable user',
      });
      if (!ok) return;
    }
    update.mutate(
      { id: u.id, disabled: !u.disabled },
      { onSuccess: () => notifySuccess(u.disabled ? 'User enabled' : 'User disabled', u.username) },
    );
  };

  const del = async (u: User) => {
    const ok = await confirmAction({
      title: `Delete user ${u.username}`,
      message: 'The account is deleted. Audit entries keep the user name.',
      confirmLabel: 'Delete user',
      typeToConfirm: u.username,
      typeToConfirmWhat: 'user name',
    });
    if (ok) remove.mutate(u.id, { onSuccess: () => notifySuccess('User deleted', u.username) });
  };

  const list = [...(users.data ?? [])].sort((a, b) => a.username.localeCompare(b.username));

  return (
    <>
      <PageHeader
        title="Users"
        description="Dashboard and API accounts"
        actions={
          <Button leftSection={<IconPlus size={16} />} onClick={() => setCreating(true)}>
            Create user
          </Button>
        }
      />
      {creating && <CreateUserModal onClose={() => setCreating(false)} />}
      {resetting && <ResetPasswordModal user={resetting} onClose={() => setResetting(null)} />}

      {users.isPending && <PageLoader />}
      {users.error && !users.data && <QueryError error={users.error} onRetry={() => void users.refetch()} />}
      {users.data && (
        <Paper withBorder radius="md">
          <Table.ScrollContainer minWidth={800}>
            <Table highlightOnHover verticalSpacing="sm">
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>User</Table.Th>
                  <Table.Th>Role</Table.Th>
                  <Table.Th>Enabled</Table.Th>
                  <Table.Th>Created</Table.Th>
                  <Table.Th>Updated</Table.Th>
                  <Table.Th w={48} />
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {list.map((u) => {
                  const self = u.id === me?.id;
                  return (
                    <Table.Tr key={u.id}>
                      <Table.Td>
                        <Group gap={6}>
                          <Text size="sm" fw={600}>
                            {u.username}
                          </Text>
                          {self && (
                            <Badge size="xs" variant="outline">
                              you
                            </Badge>
                          )}
                        </Group>
                      </Table.Td>
                      <Table.Td>
                        <Tooltip label="You cannot change your own role" disabled={!self} withArrow>
                          <Select
                            size="xs"
                            w={140}
                            data={selectData(ROLE_LABELS)}
                            value={u.role}
                            onChange={(v) => v && void changeRole(u, v as Role)}
                            allowDeselect={false}
                            disabled={self}
                          />
                        </Tooltip>
                      </Table.Td>
                      <Table.Td>
                        <Switch
                          checked={!u.disabled}
                          onChange={() => void toggleDisabled(u)}
                          disabled={self}
                          aria-label={u.disabled ? 'Enable user' : 'Disable user'}
                        />
                      </Table.Td>
                      <Table.Td>
                        <RelativeTime value={u.createdAt} />
                      </Table.Td>
                      <Table.Td>
                        <RelativeTime value={u.updatedAt} />
                      </Table.Td>
                      <Table.Td>
                        <Menu position="bottom-end" withArrow>
                          <Menu.Target>
                            <ActionIcon variant="subtle" color="gray" aria-label={`Actions for ${u.username}`}>
                              <IconDotsVertical size={16} />
                            </ActionIcon>
                          </Menu.Target>
                          <Menu.Dropdown>
                            <Menu.Item leftSection={<IconKey size={16} />} onClick={() => setResetting(u)}>
                              Reset password
                            </Menu.Item>
                            <Menu.Item
                              color="red"
                              leftSection={<IconTrash size={16} />}
                              disabled={self}
                              onClick={() => void del(u)}
                            >
                              Delete
                            </Menu.Item>
                          </Menu.Dropdown>
                        </Menu>
                      </Table.Td>
                    </Table.Tr>
                  );
                })}
              </Table.Tbody>
            </Table>
          </Table.ScrollContainer>
        </Paper>
      )}
    </>
  );
}
