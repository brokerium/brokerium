// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import {
  Badge,
  Box,
  Button,
  Group,
  NumberInput,
  Paper,
  Select,
  Stack,
  Table,
  Text,
  TextInput,
} from '@mantine/core';
import { IconChevronDown, IconChevronRight, IconPlayerPlay, IconRefresh } from '@tabler/icons-react';
import { Fragment, useState } from 'react';

import { type MessageFrom, type MessageQuery, useMessages } from '../../../api/topics';
import type { Message } from '../../../api/types';
import { Base64Badge } from '../../../components/badges';
import { JsonBlock } from '../../../components/JsonBlock';
import { PageLoader, QueryError } from '../../../components/QueryState';
import { formatDateTime, formatNumber } from '../../../lib/format';
import { previewValue, renderValue } from '../../../lib/messages';

const FROM_OPTIONS: { value: MessageFrom; label: string }[] = [
  { value: 'latest', label: 'Latest records' },
  { value: 'earliest', label: 'Earliest records' },
  { value: 'offset', label: 'From offset' },
  { value: 'timestamp', label: 'From timestamp' },
];

function ValueView({ label, value, encoding }: { label: string; value: string | null; encoding: 'utf8' | 'base64' }) {
  const rendered = renderValue(value, encoding);
  return (
    <Stack gap={4}>
      <Group gap={6}>
        <Text size="xs" fw={600} c="dimmed" tt="uppercase">
          {label}
        </Text>
        {rendered.kind === 'json' && (
          <Badge size="xs" variant="light" color="teal">
            JSON
          </Badge>
        )}
        {rendered.kind === 'binary' && <Base64Badge />}
      </Group>
      {rendered.kind === 'empty' ? (
        <Text size="sm" c="dimmed">
          null
        </Text>
      ) : (
        <JsonBlock value={rendered.text} maxHeight={420} />
      )}
    </Stack>
  );
}

function MessageDetail({ message }: { message: Message }) {
  return (
    <Stack gap="sm" p="sm">
      <ValueView label="Key" value={message.key} encoding={message.keyEncoding} />
      <ValueView label="Value" value={message.value} encoding={message.valueEncoding} />
      {message.headers.length > 0 && (
        <Stack gap={4}>
          <Text size="xs" fw={600} c="dimmed" tt="uppercase">
            Headers
          </Text>
          <Table withTableBorder verticalSpacing={4}>
            <Table.Tbody>
              {message.headers.map((h, i) => (
                <Table.Tr key={`${h.key}-${i}`}>
                  <Table.Td w="30%">
                    <Text size="sm" ff="monospace">
                      {h.key}
                    </Text>
                  </Table.Td>
                  <Table.Td>
                    <Text size="sm" ff="monospace" style={{ wordBreak: 'break-all' }}>
                      {h.value}
                    </Text>
                  </Table.Td>
                </Table.Tr>
              ))}
            </Table.Tbody>
          </Table>
        </Stack>
      )}
    </Stack>
  );
}

interface MessagesPanelProps {
  clusterId: string;
  topic: string;
  partitionCount: number;
}

export function MessagesPanel({ clusterId, topic, partitionCount }: MessagesPanelProps) {
  const [partition, setPartition] = useState('-1');
  const [from, setFrom] = useState<MessageFrom>('latest');
  const [offset, setOffset] = useState<number | ''>(0);
  const [timestamp, setTimestamp] = useState('');
  const [limit, setLimit] = useState('50');
  const [query, setQuery] = useState<MessageQuery | null>(null);
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const messages = useMessages(clusterId, topic, query);

  const timestampMs = timestamp ? new Date(timestamp).getTime() : Number.NaN;
  const invalid = (from === 'offset' && offset === '') || (from === 'timestamp' && !Number.isFinite(timestampMs));

  const fetchMessages = () => {
    const next: MessageQuery = {
      partition: Number(partition),
      from,
      offset: from === 'offset' && offset !== '' ? offset : undefined,
      timestamp: from === 'timestamp' ? timestampMs : undefined,
      limit: Number(limit),
    };
    setExpanded(new Set());
    if (JSON.stringify(next) === JSON.stringify(query)) void messages.refetch();
    else setQuery(next);
  };

  const toggle = (key: string) =>
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });

  return (
    <Stack gap="md">
      <Paper withBorder radius="md" p="sm">
        <Group align="flex-end" gap="sm">
          <Select
            label="Partition"
            w={140}
            data={[{ value: '-1', label: 'All' }, ...Array.from({ length: partitionCount }, (_, i) => String(i))]}
            value={partition}
            onChange={(v) => setPartition(v ?? '-1')}
            allowDeselect={false}
          />
          <Select
            label="Start"
            w={180}
            data={FROM_OPTIONS}
            value={from}
            onChange={(v) => v && setFrom(v as MessageFrom)}
            allowDeselect={false}
          />
          {from === 'offset' && (
            <NumberInput label="Offset" w={160} min={0} value={offset} onChange={(v) => setOffset(v === '' ? '' : Number(v))} />
          )}
          {from === 'timestamp' && (
            <TextInput
              label="Timestamp (local time)"
              type="datetime-local"
              step={1}
              w={230}
              value={timestamp}
              onChange={(e) => setTimestamp(e.currentTarget.value)}
            />
          )}
          <Select
            label="Limit"
            w={100}
            data={['10', '50', '100', '500']}
            value={limit}
            onChange={(v) => setLimit(v ?? '50')}
            allowDeselect={false}
          />
          <Button
            leftSection={query ? <IconRefresh size={16} /> : <IconPlayerPlay size={16} />}
            onClick={fetchMessages}
            loading={messages.isFetching}
            disabled={invalid}
          >
            {query ? 'Fetch again' : 'Fetch'}
          </Button>
        </Group>
      </Paper>

      {!query && (
        <Text size="sm" c="dimmed">
          Choose where to start and fetch records. Values that are not valid UTF-8 are shown as base64.
        </Text>
      )}
      {query && messages.isPending && <PageLoader label="Reading records" />}
      {messages.error && <QueryError error={messages.error} title="Could not read records" />}
      {messages.data && (
        <Paper withBorder radius="md">
          <Table.ScrollContainer minWidth={900}>
            <Table verticalSpacing="xs" highlightOnHover>
              <Table.Thead>
                <Table.Tr>
                  <Table.Th w={32} />
                  <Table.Th>Partition</Table.Th>
                  <Table.Th>Offset</Table.Th>
                  <Table.Th>Timestamp</Table.Th>
                  <Table.Th>Key</Table.Th>
                  <Table.Th>Value</Table.Th>
                  <Table.Th>Headers</Table.Th>
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {messages.data.map((m) => {
                  const key = `${m.partition}-${m.offset}`;
                  const open = expanded.has(key);
                  return (
                    <Fragment key={key}>
                      <Table.Tr style={{ cursor: 'pointer' }} onClick={() => toggle(key)}>
                        <Table.Td>{open ? <IconChevronDown size={14} /> : <IconChevronRight size={14} />}</Table.Td>
                        <Table.Td>{m.partition}</Table.Td>
                        <Table.Td>
                          <Text size="sm" ff="monospace">
                            {formatNumber(m.offset)}
                          </Text>
                        </Table.Td>
                        <Table.Td>
                          <Text size="sm" style={{ whiteSpace: 'nowrap' }}>
                            {formatDateTime(m.timestamp)}
                          </Text>
                        </Table.Td>
                        <Table.Td maw={200}>
                          <Group gap={4} wrap="nowrap">
                            {m.keyEncoding === 'base64' && <Base64Badge />}
                            <Text size="sm" ff="monospace" truncate="end">
                              {m.key === null ? <Text span c="dimmed">null</Text> : previewValue(m.key, 60)}
                            </Text>
                          </Group>
                        </Table.Td>
                        <Table.Td maw={480}>
                          <Group gap={4} wrap="nowrap">
                            {m.valueEncoding === 'base64' && <Base64Badge />}
                            <Text size="sm" ff="monospace" truncate="end">
                              {m.value === null ? <Text span c="dimmed">null</Text> : previewValue(m.value)}
                            </Text>
                          </Group>
                        </Table.Td>
                        <Table.Td>{m.headers.length || '—'}</Table.Td>
                      </Table.Tr>
                      {open && (
                        <Table.Tr>
                          <Table.Td colSpan={7} p={0}>
                            <Box bg="var(--mantine-color-default-hover)">
                              <MessageDetail message={m} />
                            </Box>
                          </Table.Td>
                        </Table.Tr>
                      )}
                    </Fragment>
                  );
                })}
                {messages.data.length === 0 && (
                  <Table.Tr>
                    <Table.Td colSpan={7}>
                      <Text size="sm" c="dimmed" ta="center" py="md">
                        No records found.
                      </Text>
                    </Table.Td>
                  </Table.Tr>
                )}
              </Table.Tbody>
            </Table>
          </Table.ScrollContainer>
        </Paper>
      )}
    </Stack>
  );
}
