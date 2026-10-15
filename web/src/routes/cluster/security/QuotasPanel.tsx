// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import {
  ActionIcon,
  Badge,
  Button,
  Checkbox,
  Group,
  Modal,
  NumberInput,
  SegmentedControl,
  Stack,
  Table,
  Text,
  TextInput,
  Tooltip,
} from '@mantine/core';
import { IconPencil, IconPlus, IconTrash } from '@tabler/icons-react';
import { useState } from 'react';

import { useAlterQuota, useQuotas } from '../../../api/security';
import type { QuotaEntity, QuotaEntry } from '../../../api/types';
import { confirmAction } from '../../../components/confirm';
import { GatedButton } from '../../../components/Gated';
import { notifySuccess } from '../../../components/notify';
import { PageLoader, QueryError } from '../../../components/QueryState';
import type { Availability } from '../../../lib/capabilities';
import { formatBytesPerSecond, formatNumber } from '../../../lib/format';
import {
  QUOTA_KEYS,
  type QuotaEntityKind,
  quotaChanges,
  quotaEntityKey,
  quotaEntityKind,
} from '../../../lib/quotas';

interface NamePart {
  name: string;
  isDefault: boolean;
}

interface Draft {
  existing: boolean;
  kind: QuotaEntityKind;
  user: NamePart;
  client: NamePart;
  ip: NamePart;
  before: Record<string, number>;
  values: Record<string, number | null>;
}

function formatQuotaValue(key: string, value: number): string {
  const def = QUOTA_KEYS.find((k) => k.key === key);
  if (def?.unit === 'bytes/s') return formatBytesPerSecond(value);
  if (def?.unit === '%') return `${formatNumber(value)}%`;
  return `${formatNumber(value)}/s`;
}

function emptyDraft(): Draft {
  return {
    existing: false,
    kind: 'user',
    user: { name: '', isDefault: false },
    client: { name: '', isDefault: false },
    ip: { name: '', isDefault: false },
    before: {},
    values: {},
  };
}

function draftFromEntry(entry: QuotaEntry): Draft {
  const part = (type: QuotaEntity['type']): NamePart => {
    const e = entry.entity.find((x) => x.type === type);
    return { name: e?.name ?? '', isDefault: e !== undefined && e.name === null };
  };
  return {
    existing: true,
    kind: quotaEntityKind(entry.entity),
    user: part('user'),
    client: part('client-id'),
    ip: part('ip'),
    before: entry.values,
    values: { ...entry.values },
  };
}

function entityFromDraft(d: Draft): QuotaEntity[] | null {
  const part = (type: QuotaEntity['type'], p: NamePart): QuotaEntity | null =>
    p.isDefault ? { type, name: null } : p.name.trim() ? { type, name: p.name.trim() } : null;
  const parts: (QuotaEntity | null)[] =
    d.kind === 'user'
      ? [part('user', d.user)]
      : d.kind === 'client-id'
        ? [part('client-id', d.client)]
        : d.kind === 'user+client-id'
          ? [part('user', d.user), part('client-id', d.client)]
          : [part('ip', d.ip)];
  return parts.every((p) => p !== null) ? (parts as QuotaEntity[]) : null;
}

function NameField({ label, part, onChange }: { label: string; part: NamePart; onChange: (p: NamePart) => void }) {
  return (
    <Group align="flex-end" gap="sm" wrap="nowrap">
      <TextInput
        label={label}
        value={part.isDefault ? '' : part.name}
        placeholder={part.isDefault ? 'Default for all' : undefined}
        disabled={part.isDefault}
        onChange={(e) => onChange({ ...part, name: e.currentTarget.value })}
        style={{ flex: 1 }}
      />
      <Checkbox label="Default" mb={8} checked={part.isDefault} onChange={(e) => onChange({ ...part, isDefault: e.currentTarget.checked })} />
    </Group>
  );
}

export function QuotasPanel({ clusterId, editable }: { clusterId: string; editable: Availability }) {
  const quotas = useQuotas(clusterId);
  const alter = useAlterQuota(clusterId);
  const [draft, setDraft] = useState<Draft | null>(null);

  if (quotas.isPending) return <PageLoader />;
  if (quotas.error) return <QueryError error={quotas.error} onRetry={() => void quotas.refetch()} />;

  const del = async (entry: QuotaEntry) => {
    const label = entry.entity.map((e) => `${e.type} ${e.name ?? '(default)'}`).join(', ');
    const ok = await confirmAction({
      title: 'Remove quota',
      message: `Remove every quota value of ${label}?`,
      confirmLabel: 'Remove quota',
    });
    if (!ok) return;
    alter.mutate(
      { entity: entry.entity, set: {}, remove: Object.keys(entry.values) },
      { onSuccess: () => notifySuccess('Quota removed', label) },
    );
  };

  const entity = draft ? entityFromDraft(draft) : null;
  const changes = draft ? quotaChanges(draft.before, draft.values) : { set: {}, remove: [] };
  const nothingChanged = Object.keys(changes.set).length === 0 && changes.remove.length === 0;

  const save = () => {
    if (!draft || !entity) return;
    alter.mutate(
      { entity, ...changes },
      {
        onSuccess: () => {
          notifySuccess('Quota saved');
          setDraft(null);
        },
      },
    );
  };

  const entries = [...quotas.data].sort((a, b) => quotaEntityKey(a.entity).localeCompare(quotaEntityKey(b.entity)));

  return (
    <Stack gap="sm">
      <Group justify="space-between">
        <Text size="sm" c="dimmed">
          Client quotas by user, client ID or IP address; default entities apply to everyone without a specific quota.
        </Text>
        <GatedButton availability={editable} leftSection={<IconPlus size={16} />} onClick={() => setDraft(emptyDraft())}>
          Add quota
        </GatedButton>
      </Group>
      <Table.ScrollContainer minWidth={700}>
        <Table striped highlightOnHover verticalSpacing="xs">
          <Table.Thead>
            <Table.Tr>
              <Table.Th>Entity</Table.Th>
              <Table.Th>Values</Table.Th>
              <Table.Th w={90} />
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {entries.map((entry) => (
              <Table.Tr key={quotaEntityKey(entry.entity)}>
                <Table.Td>
                  <Group gap={4}>
                    {entry.entity.map((e) => (
                      <Badge key={e.type} variant="light" color={e.name === null ? 'grape' : 'indigo'} size="sm">
                        {e.type}: {e.name ?? 'default'}
                      </Badge>
                    ))}
                  </Group>
                </Table.Td>
                <Table.Td>
                  <Stack gap={0}>
                    {Object.entries(entry.values).map(([key, value]) => (
                      <Text key={key} size="sm">
                        <Text span ff="monospace" size="sm" c="dimmed">
                          {key}
                        </Text>{' '}
                        {formatQuotaValue(key, value)}
                      </Text>
                    ))}
                  </Stack>
                </Table.Td>
                <Table.Td>
                  <Group gap={2} wrap="nowrap">
                    <Tooltip label={editable.allowed ? 'Edit' : editable.reason} withArrow>
                      <ActionIcon variant="subtle" disabled={!editable.allowed} onClick={() => setDraft(draftFromEntry(entry))} aria-label="Edit quota">
                        <IconPencil size={14} />
                      </ActionIcon>
                    </Tooltip>
                    <Tooltip label={editable.allowed ? 'Remove' : editable.reason} withArrow>
                      <ActionIcon
                        variant="subtle"
                        color="red"
                        disabled={!editable.allowed}
                        onClick={() => void del(entry)}
                        aria-label="Remove quota"
                      >
                        <IconTrash size={14} />
                      </ActionIcon>
                    </Tooltip>
                  </Group>
                </Table.Td>
              </Table.Tr>
            ))}
            {entries.length === 0 && (
              <Table.Tr>
                <Table.Td colSpan={3}>
                  <Text size="sm" c="dimmed" ta="center" py="md">
                    No quotas.
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
        title={<Text fw={600}>{draft?.existing ? 'Edit quota' : 'Add quota'}</Text>}
        size="lg"
      >
        {draft && (
          <form
            onSubmit={(e) => {
              e.preventDefault();
              save();
            }}
          >
            <Stack>
              <SegmentedControl
                fullWidth
                disabled={draft.existing}
                value={draft.kind}
                onChange={(v) => setDraft({ ...draft, kind: v as QuotaEntityKind, values: {} })}
                data={[
                  { value: 'user', label: 'User' },
                  { value: 'client-id', label: 'Client ID' },
                  { value: 'user+client-id', label: 'User + client ID' },
                  { value: 'ip', label: 'IP address' },
                ]}
              />
              {(draft.kind === 'user' || draft.kind === 'user+client-id') && (
                <NameField label="User" part={draft.user} onChange={(user) => setDraft({ ...draft, user })} />
              )}
              {(draft.kind === 'client-id' || draft.kind === 'user+client-id') && (
                <NameField label="Client ID" part={draft.client} onChange={(client) => setDraft({ ...draft, client })} />
              )}
              {draft.kind === 'ip' && <NameField label="IP address" part={draft.ip} onChange={(ip) => setDraft({ ...draft, ip })} />}
              {QUOTA_KEYS.filter((k) => k.kinds.includes(draft.kind)).map((k) => (
                <NumberInput
                  key={k.key}
                  label={`${k.label} (${k.unit})`}
                  description={k.key}
                  placeholder="Not set"
                  min={0}
                  value={draft.values[k.key] ?? ''}
                  onChange={(v) =>
                    setDraft({ ...draft, values: { ...draft.values, [k.key]: v === '' ? null : Number(v) } })
                  }
                  thousandSeparator=","
                />
              ))}
              <Text size="xs" c="dimmed">
                Clear a field to remove that quota value.
              </Text>
              <Group justify="flex-end">
                <Button variant="default" onClick={() => setDraft(null)}>
                  Cancel
                </Button>
                <Button type="submit" loading={alter.isPending} disabled={!entity || nothingChanged}>
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
