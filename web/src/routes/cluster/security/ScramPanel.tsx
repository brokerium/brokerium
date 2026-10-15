// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import {
  ActionIcon,
  Badge,
  Button,
  Group,
  Modal,
  NumberInput,
  PasswordInput,
  Select,
  Stack,
  Table,
  Text,
  TextInput,
  Tooltip,
} from '@mantine/core';
import { IconKey, IconPlus, IconTrash } from '@tabler/icons-react';
import { useState } from 'react';

import { type ScramMechanism, useDeleteScramUser, useScramUsers, useUpsertScramUser } from '../../../api/security';
import { confirmAction } from '../../../components/confirm';
import { GatedButton } from '../../../components/Gated';
import { notifySuccess } from '../../../components/notify';
import { PageLoader, QueryError } from '../../../components/QueryState';
import type { Availability } from '../../../lib/capabilities';

interface Draft {
  user: string;
  mechanism: ScramMechanism;
  password: string;
  iterations: number;
  existing: boolean;
}

const MECHANISMS: ScramMechanism[] = ['SCRAM-SHA-512', 'SCRAM-SHA-256'];

export function ScramPanel({ clusterId, editable }: { clusterId: string; editable: Availability }) {
  const users = useScramUsers(clusterId);
  const upsert = useUpsertScramUser(clusterId);
  const remove = useDeleteScramUser(clusterId);
  const [draft, setDraft] = useState<Draft | null>(null);

  if (users.isPending) return <PageLoader />;
  if (users.error) return <QueryError error={users.error} onRetry={() => void users.refetch()} />;

  const del = async (user: string, mechanism: ScramMechanism) => {
    const ok = await confirmAction({
      title: 'Delete SCRAM credential',
      message: `Delete the ${mechanism} credential of ${user}? Clients using it can no longer authenticate.`,
      confirmLabel: 'Delete credential',
      typeToConfirm: user,
      typeToConfirmWhat: 'user name',
    });
    if (ok) remove.mutate({ user, mechanism }, { onSuccess: () => notifySuccess('Credential deleted', `${user} (${mechanism})`) });
  };

  const save = () => {
    if (!draft || !draft.user.trim() || !draft.password) return;
    upsert.mutate(
      { user: draft.user.trim(), mechanism: draft.mechanism, password: draft.password, iterations: draft.iterations },
      {
        onSuccess: () => {
          notifySuccess(draft.existing ? 'Password updated' : 'SCRAM user created', draft.user.trim());
          setDraft(null);
        },
      },
    );
  };

  const list = [...users.data].sort((a, b) => a.user.localeCompare(b.user));

  return (
    <Stack gap="sm">
      <Group justify="space-between">
        <Text size="sm" c="dimmed">
          SCRAM credentials stored in the cluster metadata. Passwords are write-only.
        </Text>
        <GatedButton
          availability={editable}
          leftSection={<IconPlus size={16} />}
          onClick={() => setDraft({ user: '', mechanism: 'SCRAM-SHA-512', password: '', iterations: 8192, existing: false })}
        >
          Add user
        </GatedButton>
      </Group>
      <Table.ScrollContainer minWidth={640}>
        <Table striped highlightOnHover verticalSpacing="xs">
          <Table.Thead>
            <Table.Tr>
              <Table.Th>User</Table.Th>
              <Table.Th>Credentials</Table.Th>
              <Table.Th w={140} />
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {list.map((u) => (
              <Table.Tr key={u.user}>
                <Table.Td>
                  <Text size="sm" ff="monospace">
                    {u.user}
                  </Text>
                </Table.Td>
                <Table.Td>
                  <Group gap={6}>
                    {u.mechanisms.map((m) => (
                      <Badge
                        key={m.mechanism}
                        variant="light"
                        size="sm"
                        rightSection={
                          editable.allowed ? (
                            <ActionIcon
                              size={14}
                              variant="transparent"
                              color="red"
                              onClick={() => void del(u.user, m.mechanism)}
                              aria-label={`Delete ${m.mechanism}`}
                            >
                              <IconTrash size={10} />
                            </ActionIcon>
                          ) : undefined
                        }
                      >
                        {m.mechanism} · {m.iterations} iterations
                      </Badge>
                    ))}
                  </Group>
                </Table.Td>
                <Table.Td>
                  <Tooltip label={editable.allowed ? 'Set a new password' : editable.reason} withArrow>
                    <Button
                      size="xs"
                      variant="subtle"
                      leftSection={<IconKey size={14} />}
                      disabled={!editable.allowed}
                      onClick={() =>
                        setDraft({
                          user: u.user,
                          mechanism: u.mechanisms[0]?.mechanism ?? 'SCRAM-SHA-512',
                          password: '',
                          iterations: u.mechanisms[0]?.iterations ?? 8192,
                          existing: true,
                        })
                      }
                    >
                      Set password
                    </Button>
                  </Tooltip>
                </Table.Td>
              </Table.Tr>
            ))}
            {list.length === 0 && (
              <Table.Tr>
                <Table.Td colSpan={3}>
                  <Text size="sm" c="dimmed" ta="center" py="md">
                    No SCRAM users.
                  </Text>
                </Table.Td>
              </Table.Tr>
            )}
          </Table.Tbody>
        </Table>
      </Table.ScrollContainer>

      <Modal
        opened={draft !== null}
        onClose={() => setDraft(null)}
        title={<Text fw={600}>{draft?.existing ? `Set password for ${draft.user}` : 'Add SCRAM user'}</Text>}
        centered
      >
        {draft && (
          <form
            onSubmit={(e) => {
              e.preventDefault();
              save();
            }}
          >
            <Stack>
              <TextInput
                label="User"
                value={draft.user}
                disabled={draft.existing}
                onChange={(e) => setDraft({ ...draft, user: e.currentTarget.value })}
                data-autofocus={!draft.existing || undefined}
              />
              <Select
                label="Mechanism"
                data={MECHANISMS}
                value={draft.mechanism}
                onChange={(v) => v && setDraft({ ...draft, mechanism: v as ScramMechanism })}
                allowDeselect={false}
              />
              <PasswordInput
                label="Password"
                value={draft.password}
                onChange={(e) => setDraft({ ...draft, password: e.currentTarget.value })}
                autoComplete="new-password"
                data-autofocus={draft.existing || undefined}
              />
              <NumberInput
                label="Iterations"
                description="Kafka requires at least 4096"
                min={4096}
                max={16384}
                value={draft.iterations}
                onChange={(v) => setDraft({ ...draft, iterations: Number(v) || 8192 })}
              />
              <Group justify="flex-end">
                <Button variant="default" onClick={() => setDraft(null)}>
                  Cancel
                </Button>
                <Button type="submit" loading={upsert.isPending} disabled={!draft.user.trim() || !draft.password}>
                  Save
                </Button>
              </Group>
            </Stack>
          </form>
        )}
      </Modal>
    </Stack>
  );
}
