// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { ActionIcon, Button, Group, Paper, Select, Stack, Table, Text, Textarea, TextInput } from '@mantine/core';
import { IconBraces, IconPlus, IconSend, IconTrash } from '@tabler/icons-react';
import { useState } from 'react';

import { type ProduceResponse, useProduceMessage } from '../../../api/topics';
import { GatedButton } from '../../../components/Gated';
import { notifySuccess } from '../../../components/notify';
import type { Availability } from '../../../lib/capabilities';
import { formatNumber } from '../../../lib/format';

interface HeaderRow {
  id: number;
  key: string;
  value: string;
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
  const [value, setValue] = useState('');
  const [headers, setHeaders] = useState<HeaderRow[]>([]);
  const [nextId, setNextId] = useState(1);
  const [results, setResults] = useState<(ProduceResponse & { at: number })[]>([]);

  let jsonError: string | null = null;
  const trimmed = value.trim();
  if (trimmed.startsWith('{') || trimmed.startsWith('[')) {
    try {
      JSON.parse(trimmed);
    } catch (err) {
      jsonError = err instanceof Error ? err.message : 'Invalid JSON';
    }
  }

  const formatJson = () => {
    try {
      setValue(JSON.stringify(JSON.parse(value), null, 2));
    } catch {
      // Leave non-JSON values untouched.
    }
  };

  const submit = () => {
    produce.mutate(
      {
        partition: partition === 'any' ? undefined : Number(partition),
        key: key === '' ? undefined : key,
        value,
        headers: headers.filter((h) => h.key.trim()).map((h) => ({ key: h.key.trim(), value: h.value })),
      },
      {
        onSuccess: (res) => {
          notifySuccess('Record produced', `Partition ${res.partition}, offset ${formatNumber(res.offset)}`);
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
                styles={{ input: { fontFamily: 'var(--mantine-font-family-monospace)' } }}
              />
            </Group>
            <Textarea
              label="Value"
              autosize
              minRows={4}
              maxRows={16}
              value={value}
              onChange={(e) => setValue(e.currentTarget.value)}
              error={jsonError ? `Looks like JSON but does not parse: ${jsonError}` : undefined}
              styles={{ input: { fontFamily: 'var(--mantine-font-family-monospace)', fontSize: 13 } }}
              spellCheck={false}
            />
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
                    setHeaders([...headers, { id: nextId, key: '', value: '' }]);
                    setNextId(nextId + 1);
                  }}
                >
                  Add header
                </Button>
              </Group>
              {headers.map((h) => (
                <Group key={h.id} gap="xs" wrap="nowrap">
                  <TextInput
                    placeholder="key"
                    value={h.key}
                    onChange={(e) => {
                      const k = e.currentTarget.value;
                      setHeaders(headers.map((x) => (x.id === h.id ? { ...x, key: k } : x)));
                    }}
                    style={{ flex: 1 }}
                  />
                  <TextInput
                    placeholder="value"
                    value={h.value}
                    onChange={(e) => {
                      const v = e.currentTarget.value;
                      setHeaders(headers.map((x) => (x.id === h.id ? { ...x, value: v } : x)));
                    }}
                    style={{ flex: 2 }}
                  />
                  <ActionIcon
                    variant="subtle"
                    color="red"
                    onClick={() => setHeaders(headers.filter((x) => x.id !== h.id))}
                    aria-label="Remove header"
                  >
                    <IconTrash size={14} />
                  </ActionIcon>
                </Group>
              ))}
            </Stack>
            <Group justify="space-between">
              <Button variant="default" leftSection={<IconBraces size={16} />} onClick={formatJson} disabled={!trimmed || jsonError !== null}>
                Format JSON
              </Button>
              <GatedButton
                type="submit"
                availability={availability}
                leftSection={<IconSend size={16} />}
                loading={produce.isPending}
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
