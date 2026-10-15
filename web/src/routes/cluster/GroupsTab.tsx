// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { Anchor, Badge, Group, Paper, Stack, Table, Text, TextInput, Tooltip } from '@mantine/core';
import { IconSearch } from '@tabler/icons-react';
import { useMemo, useState } from 'react';
import { Link } from 'react-router';

import { formatNumber } from '../../lib/format';
import { groupStateColor } from '../../lib/groups';
import { useClusterContext } from './clusterContext';
import { NoSnapshot } from './common';

export default function GroupsTab() {
  const { clusterId, snapshot } = useClusterContext();
  const [search, setSearch] = useState('');

  const groups = useMemo(() => {
    const q = search.trim().toLowerCase();
    return (snapshot?.groups ?? [])
      .filter((g) => !q || g.id.toLowerCase().includes(q) || (g.topics ?? []).some((t) => t.toLowerCase().includes(q)))
      .sort((a, b) => b.lag - a.lag || a.id.localeCompare(b.id));
  }, [snapshot, search]);

  if (!snapshot) return <NoSnapshot />;
  const base = `/clusters/${encodeURIComponent(clusterId)}/groups`;

  return (
    <Stack gap="md">
      <Group>
        <TextInput
          placeholder="Search by group or topic"
          leftSection={<IconSearch size={14} />}
          value={search}
          onChange={(e) => setSearch(e.currentTarget.value)}
          w={300}
        />
      </Group>
      <Paper withBorder radius="md">
        <Table.ScrollContainer minWidth={800}>
          <Table highlightOnHover verticalSpacing="xs">
            <Table.Thead>
              <Table.Tr>
                <Table.Th>Group</Table.Th>
                <Table.Th>State</Table.Th>
                <Table.Th>Type</Table.Th>
                <Table.Th ta="right">Members</Table.Th>
                <Table.Th ta="right">Lag</Table.Th>
                <Table.Th>Topics</Table.Th>
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {groups.map((g) => {
                const topics = g.topics ?? [];
                return (
                  <Table.Tr key={g.id}>
                    <Table.Td>
                      <Group gap={6} wrap="nowrap">
                        <Anchor component={Link} to={`${base}/${encodeURIComponent(g.id)}`} size="sm" ff="monospace">
                          {g.id}
                        </Anchor>
                        {g.error && (
                          <Badge size="xs" color="red" variant="light" title={g.error}>
                            error
                          </Badge>
                        )}
                      </Group>
                    </Table.Td>
                    <Table.Td>
                      <Badge size="sm" variant="light" color={groupStateColor(g.state)}>
                        {g.state || 'unknown'}
                      </Badge>
                    </Table.Td>
                    <Table.Td>
                      <Badge size="sm" variant="outline" color={g.type === 'share' ? 'grape' : 'gray'}>
                        {g.type === 'share' ? 'share' : g.protocolType || 'consumer'}
                      </Badge>
                    </Table.Td>
                    <Table.Td ta="right">{formatNumber(g.members)}</Table.Td>
                    <Table.Td ta="right">
                      <Text size="sm" fw={g.lag > 0 ? 600 : undefined} c={g.lag > 0 ? 'orange.7' : undefined}>
                        {formatNumber(g.lag)}
                      </Text>
                    </Table.Td>
                    <Table.Td maw={320}>
                      <Tooltip label={topics.join(', ')} disabled={topics.length <= 2} multiline maw={400} withArrow>
                        <Text size="sm" truncate="end" ff="monospace">
                          {topics.length === 0 ? '—' : topics.slice(0, 2).join(', ')}
                          {topics.length > 2 ? ` +${topics.length - 2}` : ''}
                        </Text>
                      </Tooltip>
                    </Table.Td>
                  </Table.Tr>
                );
              })}
              {groups.length === 0 && (
                <Table.Tr>
                  <Table.Td colSpan={6}>
                    <Text size="sm" c="dimmed" ta="center" py="md">
                      No consumer groups.
                    </Text>
                  </Table.Td>
                </Table.Tr>
              )}
            </Table.Tbody>
          </Table>
        </Table.ScrollContainer>
      </Paper>
    </Stack>
  );
}
