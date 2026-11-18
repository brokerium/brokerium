// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { Alert, Button, CloseButton, Code, Group, Loader, Paper, Stack, Text } from '@mantine/core';
import { IconInfoCircle, IconPlus } from '@tabler/icons-react';
import { useState } from 'react';

import { useCreateJoinToken } from '../../api/nodes';
import type { JoinTokenResponse } from '../../api/types';
import { formatDateTime } from '../../lib/format';
import { CopyIcon } from '../CopyText';
import { RelativeTime } from '../RelativeTime';

/** The install command of a join token, shown once. */
export function JoinCommand({ token }: { token: JoinTokenResponse }) {
  return (
    <Stack gap="xs">
      <Paper withBorder p="sm" radius="md" pos="relative">
        <Code block style={{ whiteSpace: 'pre-wrap', wordBreak: 'break-all', paddingRight: 36 }}>
          {token.command}
        </Code>
        <Group pos="absolute" top={8} right={8}>
          <CopyIcon value={token.command} label="Copy command" />
        </Group>
      </Paper>
      <Alert color="yellow" variant="light" icon={<IconInfoCircle size={18} />}>
        The token is shown only once and expires {formatDateTime(token.expiresAt)} (
        <RelativeTime value={token.expiresAt} size="sm" />
        ).
      </Alert>
    </Stack>
  );
}

interface AddHostProps {
  /** Cluster the new node is attached to; none for a cluster that is still being created. */
  clusterId?: string;
  /** Called with true while the command is shown, so host lists can poll for the new agent. */
  onWaitingChange?: (waiting: boolean) => void;
}

/** Creates a join token for a new host and shows its install command until dismissed. */
export function AddHost({ clusterId, onWaitingChange }: AddHostProps) {
  const create = useCreateJoinToken();
  const [token, setToken] = useState<JoinTokenResponse | null>(null);

  const open = () =>
    create.mutate(
      { clusterId, ttlMinutes: 60 },
      {
        onSuccess: (t) => {
          setToken(t);
          onWaitingChange?.(true);
        },
      },
    );
  const close = () => {
    setToken(null);
    onWaitingChange?.(false);
  };

  if (!token) {
    return (
      <Group>
        <Button variant="light" size="xs" leftSection={<IconPlus size={14} />} loading={create.isPending} onClick={open}>
          Add host
        </Button>
      </Group>
    );
  }
  return (
    <Paper withBorder p="sm" radius="md">
      <Stack gap="xs">
        <Group justify="space-between" wrap="nowrap">
          <Text size="sm" fw={500}>
            Run this command on the new host. It installs brokerium-agent and joins it with a one-time token.
          </Text>
          <CloseButton aria-label="Close" onClick={close} />
        </Group>
        <JoinCommand token={token} />
        <Group gap="xs">
          <Loader size="xs" />
          <Text size="xs" c="dimmed">
            The host appears in the list when its agent connects.
          </Text>
        </Group>
      </Stack>
    </Paper>
  );
}
