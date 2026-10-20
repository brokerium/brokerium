// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

import { Alert, Button, Group, Menu, Paper, Select, Stack, Text } from '@mantine/core';
import { IconChevronDown, IconDownload, IconPlayerTrackNext, IconSearch } from '@tabler/icons-react';
import { useState } from 'react';
import { Link } from 'react-router';

import { EnterpriseBadge } from '../../components/badges';
import { useFeature } from '../../hooks/useFeature';
import { formatNumber } from '../../lib/format';
import { MessageTable } from '../../routes/cluster/topic/MessagesPanel';
import {
  type ExportFormat,
  type SearchRequest,
  type SearchResult,
  useExportMessages,
  useSearchMessages,
} from '../api/messageTools';
import { MESSAGE_TOOLS } from '../features';
import { EMPTY_FILTER, type FilterState, filterRequest } from './filter';
import { FilterFields } from './FilterFields';

const EXPORT_FORMATS: { value: ExportFormat; label: string }[] = [
  { value: 'ndjson', label: 'NDJSON (one record per line)' },
  { value: 'json', label: 'JSON array' },
  { value: 'csv', label: 'CSV' },
];

function stoppedText(result: SearchResult): string {
  switch (result.stopped) {
    case 'limit':
      return 'enough matches found';
    case 'records':
      return 'record limit reached';
    case 'time':
      return 'time limit reached';
    case undefined:
      return result.complete ? 'searched to the end' : 'stopped';
    default: {
      const unknown: never = result.stopped;
      return unknown;
    }
  }
}

interface SearchPanelProps {
  clusterId: string;
  topic: string;
  partitionCount: number;
}

export function SearchPanel({ clusterId, topic, partitionCount }: SearchPanelProps) {
  const { availability } = useFeature(MESSAGE_TOOLS);
  const search = useSearchMessages(clusterId, topic);
  const exporter = useExportMessages(clusterId, topic);
  const [filter, setFilter] = useState<FilterState>(EMPTY_FILTER);
  const [limit, setLimit] = useState('100');
  const [request, setRequest] = useState<SearchRequest | null>(null);
  const [result, setResult] = useState<SearchResult | null>(null);

  if (!availability.allowed) {
    return (
      <Alert title={<Group gap="xs">Search and export records <EnterpriseBadge size="xs" /></Group>}>
        <Text size="sm">
          {availability.reason}. See <Link to="/license">License</Link> for the features of Brokerium Enterprise.
        </Text>
      </Alert>
    );
  }

  const run = (req: SearchRequest, append: boolean) => {
    search.mutate(req, {
      onSuccess: (res) => {
        setRequest(req);
        setResult(
          append && result
            ? { ...res, scanned: result.scanned + res.scanned, messages: [...result.messages, ...res.messages] }
            : res,
        );
      },
    });
  };

  const start = () => run({ ...filterRequest(filter), limit: Number(limit) }, false);
  const more = () => {
    if (request && result) run({ ...request, resume: result.next }, true);
  };

  return (
    <Stack gap="md">
      <Paper withBorder radius="md" p="sm">
        <Stack gap="sm">
          <FilterFields value={filter} onChange={setFilter} partitionCount={partitionCount} />
          <Group justify="space-between">
            <Group gap="sm" align="flex-end">
              <Select
                label="Matches"
                w={110}
                data={['100', '500', '1000']}
                value={limit}
                allowDeselect={false}
                onChange={(v) => setLimit(v ?? '100')}
              />
              <Button leftSection={<IconSearch size={16} />} onClick={start} loading={search.isPending}>
                Search
              </Button>
            </Group>
            <Menu position="bottom-end">
              <Menu.Target>
                <Button
                  variant="default"
                  leftSection={<IconDownload size={16} />}
                  rightSection={<IconChevronDown size={14} />}
                  loading={exporter.isPending}
                >
                  Export
                </Button>
              </Menu.Target>
              <Menu.Dropdown>
                <Menu.Label>Records that match, up to a million</Menu.Label>
                {EXPORT_FORMATS.map((f) => (
                  <Menu.Item key={f.value} onClick={() => exporter.mutate({ ...filterRequest(filter), format: f.value })}>
                    {f.label}
                  </Menu.Item>
                ))}
              </Menu.Dropdown>
            </Menu>
          </Group>
        </Stack>
      </Paper>
      {!result && (
        <Text size="sm" c="dimmed">
          Search reads the topic from the start of the time range, or from the earliest record, and stops after the
          matches, a million records or 30 seconds. Export writes every matching record to a file.
        </Text>
      )}
      {result && (
        <Paper withBorder radius="md">
          <MessageTable key={JSON.stringify(request)} messages={result.messages} />
          <Group justify="space-between" px="sm" py="xs">
            <Text size="sm" c="dimmed">
              {formatNumber(result.messages.length)} matches in {formatNumber(result.scanned)} records read;{' '}
              {stoppedText(result)}
            </Text>
            <Button
              size="xs"
              variant="default"
              rightSection={<IconPlayerTrackNext size={14} />}
              disabled={result.complete || search.isPending}
              onClick={more}
            >
              Continue
            </Button>
          </Group>
        </Paper>
      )}
    </Stack>
  );
}
