// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import {
  ActionIcon,
  Button,
  Group,
  NativeSelect,
  Paper,
  SegmentedControl,
  Select,
  Stack,
  Table,
  Text,
  Textarea,
  TextInput,
} from '@mantine/core';
import { IconBraces, IconPlus, IconSend, IconTrash } from '@tabler/icons-react';
import { useState } from 'react';

import { type ProduceResponse, useProduceMessage } from '../../../api/topics';
import type { ByteEncoding } from '../../../api/types';
import { GatedButton } from '../../../components/Gated';
import { notifySuccess } from '../../../components/notify';
import type { Availability } from '../../../lib/capabilities';
import { formatNumber } from '../../../lib/format';
import { isBase64 } from '../../../lib/messages';

type ValueMode = ByteEncoding | 'tombstone';

const ENCODINGS: { value: ByteEncoding; label: string }[] = [
  { value: 'utf8', label: 'Text' },
  { value: 'base64', label: 'Base64' },
];

const VALUE_MODES: { value: ValueMode; label: string }[] = [...ENCODINGS, { value: 'tombstone', label: 'Tombstone' }];

const ENCODING_WIDTH = 92;

interface HeaderRow {
  id: number;
  key: string;
  value: string;
  encoding: ByteEncoding;
}

function base64Error(text: string, encoding: ByteEncoding): string | undefined {
  return encoding === 'base64' && !isBase64(text) ? 'Not valid base64' : undefined;
}

function EncodingSelect({ label, value, onChange }: { label: string; value: ByteEncoding; onChange: (v: ByteEncoding) => void }) {
  return (
    <NativeSelect
      aria-label={label}
      data={ENCODINGS}
      value={value}
      onChange={(e) => onChange(e.currentTarget.value as ByteEncoding)}
      rightSectionWidth={24}
      styles={{
        input: { fontWeight: 500, borderTopLeftRadius: 0, borderBottomLeftRadius: 0, width: ENCODING_WIDTH, marginRight: -2 },
      }}
    />
  );
}

interface ProducePanelProps {
  clusterId: string;
  topic: string;
  partitionCount: number;
  availability: Availability;
}

export function ProducePanel({ clusterId, topic, partitionCount, availability }: ProducePanelProps) {
  const produce = useProduceMessage(clusterId, topic);
  const [partition, setPartition] = useState<string>('any');
  const [key, setKey] = useState('');
  const [keyEncoding, setKeyEncoding] = useState<ByteEncoding>('utf8');
  const [value, setValue] = useState('');
  const [valueMode, setValueMode] = useState<ValueMode>('utf8');
  const [headers, setHeaders] = useState<HeaderRow[]>([]);
  const [nextId, setNextId] = useState(1);
  const [results, setResults] = useState<(ProduceResponse & { at: number })[]>([]);

  const tombstone = valueMode === 'tombstone';
  const trimmed = value.trim();
  let jsonError: string | null = null;
  if (valueMode === 'utf8' && (trimmed.startsWith('{') || trimmed.startsWith('['))) {
    try {
      JSON.parse(trimmed);
    } catch (err) {
      jsonError = err instanceof Error ? err.message : 'Invalid JSON';
    }
  }
  const keyError = base64Error(key, keyEncoding);
  const valueError = valueMode === 'base64' ? base64Error(value, 'base64') : undefined;
  const headerErrors = headers.map((h) => base64Error(h.value, h.encoding));
  const invalid = Boolean(keyError || valueError || headerErrors.some(Boolean));

  const formatJson = () => {
    try {
      setValue(JSON.stringify(JSON.parse(value), null, 2));
    } catch {
      // Leave non-JSON values untouched.
    }
  };

  const updateHeader = (id: number, patch: Partial<HeaderRow>) =>
    setHeaders(headers.map((x) => (x.id === id ? { ...x, ...patch } : x)));

  const submit = () => {
    if (invalid || !availability.allowed) return;
    produce.mutate(
      {
        partition: partition === 'any' ? undefined : Number(partition),
        key: key === '' ? undefined : key,
        keyEncoding,
        value: tombstone ? null : value,
        valueEncoding: valueMode === 'base64' ? 'base64' : 'utf8',
        headers: headers
          .filter((h) => h.key.trim())
          .map((h) => ({ key: h.key.trim(), value: h.value, encoding: h.encoding })),
      },
      {
        onSuccess: (res) => {
          notifySuccess(
            tombstone ? 'Tombstone produced' : 'Record produced',
            `Partition ${res.partition}, offset ${formatNumber(res.offset)}`,
          );
          setResults((prev) => [{ ...res, at: Date.now() }, ...prev].slice(0, 10));
        },
      },
    );
  };

  return (
    <Stack gap="md">
      <Paper withBorder radius="md" p="md">
        <form
          onSubmit={(e) => {
            e.preventDefault();
            submit();
          }}
        >
          <Stack gap="sm">
            <Group grow align="flex-start">
              <Select
                label="Partition"
                data={[
                  { value: 'any', label: 'Chosen by the partitioner' },
                  ...Array.from({ length: partitionCount }, (_, i) => ({ value: String(i), label: String(i) })),
                ]}
                value={partition}
                onChange={(v) => setPartition(v ?? 'any')}
                allowDeselect={false}
              />
              <TextInput
                label="Key"
                placeholder="Optional"
                value={key}
                onChange={(e) => setKey(e.currentTarget.value)}
                error={keyError}
                rightSection={<EncodingSelect label="Key format" value={keyEncoding} onChange={setKeyEncoding} />}
                rightSectionWidth={ENCODING_WIDTH}
                styles={{ input: { fontFamily: 'var(--mantine-font-family-monospace)' } }}
              />
            </Group>
            <Stack gap={4}>
              <Group justify="space-between" align="flex-end">
                <Text size="sm" fw={500}>
                  Value
                </Text>
                <SegmentedControl
                  size="xs"
                  data={VALUE_MODES}
                  value={valueMode}
                  onChange={(v) => setValueMode(v as ValueMode)}
                />
              </Group>
              {tombstone ? (
                <Text size="sm" c="dimmed">
                  A record without a value. In a compacted topic it removes the earlier records with the same key
                  {key === '' ? '; set a key, compacted topics reject records without one.' : '.'}
                </Text>
              ) : (
                <Textarea
                  aria-label="Value"
                  autosize
                  minRows={4}
                  maxRows={16}
                  value={value}
                  placeholder={valueMode === 'base64' ? 'Base64, for example AAECAw==' : undefined}
                  onChange={(e) => setValue(e.currentTarget.value)}
                  error={valueError ?? (jsonError ? `Looks like JSON but does not parse: ${jsonError}` : undefined)}
                  styles={{ input: { fontFamily: 'var(--mantine-font-family-monospace)', fontSize: 13 } }}
                  spellCheck={false}
                />
              )}
            </Stack>
            <Stack gap="xs">
              <Group justify="space-between">
                <Text size="sm" fw={500}>
                  Headers
                </Text>
                <Button
                  size="compact-xs"
                  variant="subtle"
                  leftSection={<IconPlus size={12} />}
                  onClick={() => {
                    setHeaders([...headers, { id: nextId, key: '', value: '', encoding: 'utf8' }]);
                    setNextId(nextId + 1);
                  }}
                >
                  Add header
                </Button>
              </Group>
              {headers.map((h, i) => (
                <Group key={h.id} gap="xs" wrap="nowrap" align="flex-start">
                  <TextInput
                    placeholder="key"
                    value={h.key}
                    onChange={(e) => updateHeader(h.id, { key: e.currentTarget.value })}
                    style={{ flex: 1 }}
                  />
                  <TextInput
                    placeholder="value"
                    value={h.value}
                    onChange={(e) => updateHeader(h.id, { value: e.currentTarget.value })}
                    error={headerErrors[i]}
                    rightSection={
                      <EncodingSelect
                        label="Header value format"
                        value={h.encoding}
                        onChange={(encoding) => updateHeader(h.id, { encoding })}
                      />
                    }
                    rightSectionWidth={ENCODING_WIDTH}
                    style={{ flex: 2 }}
                  />
                  <ActionIcon
                    variant="subtle"
                    color="red"
                    mt={6}
                    onClick={() => setHeaders(headers.filter((x) => x.id !== h.id))}
                    aria-label="Remove header"
                  >
                    <IconTrash size={14} />
                  </ActionIcon>
                </Group>
              ))}
            </Stack>
            <Group justify="space-between">
              <Button
                variant="default"
                leftSection={<IconBraces size={16} />}
                onClick={formatJson}
                disabled={valueMode !== 'utf8' || !trimmed || jsonError !== null}
              >
                Format JSON
              </Button>
              <GatedButton
                type="submit"
                availability={availability}
                leftSection={<IconSend size={16} />}
                loading={produce.isPending}
                disabled={invalid}
              >
                Produce
              </GatedButton>
            </Group>
          </Stack>
        </form>
      </Paper>

      {results.length > 0 && (
        <Paper withBorder radius="md" p="sm">
          <Text size="sm" fw={600} mb="xs">
            Produced in this session
          </Text>
          <Table verticalSpacing={4}>
            <Table.Thead>
              <Table.Tr>
                <Table.Th>Partition</Table.Th>
                <Table.Th>Offset</Table.Th>
                <Table.Th>Time</Table.Th>
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {results.map((r) => (
                <Table.Tr key={`${r.partition}-${r.offset}`}>
                  <Table.Td>{r.partition}</Table.Td>
                  <Table.Td>{formatNumber(r.offset)}</Table.Td>
                  <Table.Td>{new Date(r.at).toLocaleTimeString()}</Table.Td>
                </Table.Tr>
              ))}
            </Table.Tbody>
          </Table>
        </Paper>
      )}
    </Stack>
  );
}
