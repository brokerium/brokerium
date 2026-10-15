// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { Alert, Button, Group, Modal, Paper, Stack, Text, Title } from '@mantine/core';
import { IconCloudOff, IconEye, IconLock } from '@tabler/icons-react';
import type { ReactNode } from 'react';

export function NoSnapshot() {
  return (
    <Alert color="gray" icon={<IconCloudOff size={18} />} title="No data">
      The cluster has not been reached yet, so there is no snapshot to show. Check the connection settings or refresh.
    </Alert>
  );
}

export function TabNotAvailable({ what }: { what: string }) {
  return (
    <Alert color="gray" icon={<IconLock size={18} />} title="Not available">
      {what} is not available for this cluster.
    </Alert>
  );
}

export function ReadOnlyNotice() {
  return (
    <Alert color="gray" variant="light" icon={<IconEye size={18} />}>
      Your role is read-only: actions are disabled.
    </Alert>
  );
}

interface SectionProps {
  title: ReactNode;
  description?: ReactNode;
  actions?: ReactNode;
  children: ReactNode;
}

export function Section({ title, description, actions, children }: SectionProps) {
  return (
    <Paper withBorder radius="md" p="md">
      <Group justify="space-between" align="flex-start" mb="sm" gap="sm">
        <Stack gap={2}>
          <Title order={4}>{title}</Title>
          {description && (
            <Text size="sm" c="dimmed">
              {description}
            </Text>
          )}
        </Stack>
        {actions && <Group gap="xs">{actions}</Group>}
      </Group>
      {children}
    </Paper>
  );
}

interface ParamsModalProps {
  opened: boolean;
  onClose: () => void;
  title: string;
  description?: ReactNode;
  children?: ReactNode;
  planning: boolean;
  /** Plans the operation; the dialog closes when planning succeeds. */
  onPlan: () => Promise<boolean>;
  submitLabel?: string;
  disabled?: boolean;
}

/** Collects parameters of an operation before its dry-run plan is shown. */
export function ParamsModal({
  opened,
  onClose,
  title,
  description,
  children,
  planning,
  onPlan,
  submitLabel = 'Preview plan',
  disabled = false,
}: ParamsModalProps) {
  return (
    <Modal opened={opened} onClose={onClose} title={<Text fw={600}>{title}</Text>} centered>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          void onPlan().then((ok) => {
            if (ok) onClose();
          });
        }}
      >
        <Stack>
          {description && (typeof description === 'string' ? <Text size="sm">{description}</Text> : description)}
          {children}
          <Group justify="flex-end">
            <Button variant="default" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" loading={planning} disabled={disabled}>
              {submitLabel}
            </Button>
          </Group>
        </Stack>
      </form>
    </Modal>
  );
}
