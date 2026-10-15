// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import {
  ActionIcon,
  Autocomplete,
  Badge,
  Button,
  Group,
  Modal,
  Stack,
  Switch,
  Table,
  Text,
  TextInput,
  Tooltip,
} from '@mantine/core';
import { IconArrowBackUp, IconPencil, IconPlus, IconSearch } from '@tabler/icons-react';
import { useMemo, useState } from 'react';

import type { ConfigPatch } from '../api/clusters';
import type { ConfigEntry } from '../api/types';
import type { Availability } from '../lib/capabilities';
import { confirmAction } from './confirm';
import { GatedButton } from './Gated';

interface ConfigTableProps {
  configs: ConfigEntry[];
  /** Config source that marks a per-resource override, e.g. DYNAMIC_TOPIC_CONFIG. */
  overrideSource: string;
  editable: Availability;
  saving: boolean;
  onPatch: (patch: ConfigPatch) => Promise<unknown>;
  /** What the configs belong to, for dialog texts: "topic orders", "broker 101". */
  subject: string;
}

function sourceColor(source: string, overrideSource: string): string {
  if (source === overrideSource) return 'indigo';
  if (source.startsWith('DYNAMIC')) return 'cyan';
  if (source.startsWith('STATIC')) return 'grape';
  return 'gray';
}

function sourceLabel(source: string): string {
  return source.replace(/_CONFIG$/, '').replaceAll('_', ' ').toLowerCase();
}

interface EditState {
  name: string;
  value: string;
  isNew: boolean;
}

export function ConfigTable({ configs, overrideSource, editable, saving, onPatch, subject }: ConfigTableProps) {
  const [search, setSearch] = useState('');
  const [onlyOverrides, setOnlyOverrides] = useState(false);
  const [edit, setEdit] = useState<EditState | null>(null);

  const rows = useMemo(() => {
    const q = search.trim().toLowerCase();
    return configs.filter(
      (c) =>
        (!onlyOverrides || c.source === overrideSource) &&
        (!q || c.name.toLowerCase().includes(q) || c.value.toLowerCase().includes(q)),
    );
  }, [configs, search, onlyOverrides, overrideSource]);

  const overrides = configs.filter((c) => c.source === overrideSource).length;

  const save = async () => {
    if (!edit || !edit.name.trim()) return;
    await onPatch({ set: { [edit.name.trim()]: edit.value }, delete: [] });
    setEdit(null);
  };

  const reset = async (name: string) => {
    const ok = await confirmAction({
      title: 'Remove override',
      message: `Remove the override of ${name} on ${subject}? The value falls back to the broker or cluster default.`,
      confirmLabel: 'Remove override',
    });
    if (ok) await onPatch({ set: {}, delete: [name] }).catch(() => undefined);
  };

  return (
    <Stack gap="sm">
      <Group justify="space-between">
        <Group gap="sm">
          <TextInput
            placeholder="Filter configs"
            leftSection={<IconSearch size={14} />}
            value={search}
            onChange={(e) => setSearch(e.currentTarget.value)}
            w={260}
          />
          <Switch
            label={`Only overrides (${overrides})`}
            checked={onlyOverrides}
            onChange={(e) => setOnlyOverrides(e.currentTarget.checked)}
          />
        </Group>
        <GatedButton
          availability={editable}
          size="xs"
          leftSection={<IconPlus size={14} />}
          onClick={() => setEdit({ name: '', value: '', isNew: true })}
        >
          Add override
        </GatedButton>
      </Group>
      <Table.ScrollContainer minWidth={640}>
        <Table striped highlightOnHover verticalSpacing={6}>
          <Table.Thead>
            <Table.Tr>
              <Table.Th>Name</Table.Th>
              <Table.Th>Value</Table.Th>
              <Table.Th>Source</Table.Th>
              <Table.Th w={80} />
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {rows.map((c) => (
              <Table.Tr key={c.name}>
                <Table.Td>
                  <Text size="sm" ff="monospace">
                    {c.name}
                  </Text>
                </Table.Td>
                <Table.Td maw={380}>
                  {c.sensitive ? (
                    <Badge size="xs" color="gray" variant="outline">
                      sensitive
                    </Badge>
                  ) : (
                    <Text size="sm" ff="monospace" style={{ wordBreak: 'break-all' }}>
                      {c.value === '' ? <Text span c="dimmed">(empty)</Text> : c.value}
                    </Text>
                  )}
                </Table.Td>
                <Table.Td>
                  <Badge size="xs" variant="light" color={sourceColor(c.source, overrideSource)}>
                    {sourceLabel(c.source)}
                  </Badge>
                </Table.Td>
                <Table.Td>
                  <Group gap={2} justify="flex-end" wrap="nowrap">
                    <Tooltip label={editable.allowed ? 'Edit' : editable.reason} withArrow>
                      <ActionIcon
                        variant="subtle"
                        size="sm"
                        disabled={!editable.allowed}
                        onClick={() => setEdit({ name: c.name, value: c.sensitive ? '' : c.value, isNew: false })}
                        aria-label={`Edit ${c.name}`}
                      >
                        <IconPencil size={14} />
                      </ActionIcon>
                    </Tooltip>
                    {c.source === overrideSource && (
                      <Tooltip label="Remove override" withArrow>
                        <ActionIcon
                          variant="subtle"
                          color="red"
                          size="sm"
                          disabled={!editable.allowed}
                          onClick={() => void reset(c.name)}
                          aria-label={`Remove override ${c.name}`}
                        >
                          <IconArrowBackUp size={14} />
                        </ActionIcon>
                      </Tooltip>
                    )}
                  </Group>
                </Table.Td>
              </Table.Tr>
            ))}
            {rows.length === 0 && (
              <Table.Tr>
                <Table.Td colSpan={4}>
                  <Text size="sm" c="dimmed" ta="center" py="sm">
                    No configs match the filter.
                  </Text>
                </Table.Td>
              </Table.Tr>
            )}
          </Table.Tbody>
        </Table>
      </Table.ScrollContainer>

      <Modal
        opened={edit !== null}
        onClose={() => setEdit(null)}
        title={edit?.isNew ? `Add override on ${subject}` : `Edit ${edit?.name ?? ''}`}
        centered
      >
        {edit && (
          <form
            onSubmit={(e) => {
              e.preventDefault();
              void save().catch(() => undefined);
            }}
          >
            <Stack>
              {edit.isNew ? (
                <Autocomplete
                  label="Config name"
                  placeholder="retention.ms"
                  data={configs.map((c) => c.name)}
                  value={edit.name}
                  onChange={(name) => setEdit({ ...edit, name })}
                  limit={20}
                  data-autofocus
                />
              ) : (
                <Text size="sm" ff="monospace">
                  {edit.name}
                </Text>
              )}
              <TextInput
                label="Value"
                value={edit.value}
                onChange={(e) => setEdit({ ...edit, value: e.currentTarget.value })}
                styles={{ input: { fontFamily: 'var(--mantine-font-family-monospace)' } }}
                data-autofocus={!edit.isNew || undefined}
              />
              <Text size="xs" c="dimmed">
                The value is validated by Kafka; invalid values are rejected without changing {subject}.
              </Text>
              <Group justify="flex-end">
                <Button variant="default" onClick={() => setEdit(null)}>
                  Cancel
                </Button>
                <Button type="submit" loading={saving} disabled={!edit.name.trim()}>
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
