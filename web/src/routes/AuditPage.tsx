// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { Anchor, Badge, Box, Code, Group, Paper, Select, Table, Text, TextInput, Tooltip } from '@mantine/core';
import { IconChevronDown, IconChevronRight, IconSearch } from '@tabler/icons-react';
import { Fragment, useMemo, useState } from 'react';
import { Link } from 'react-router';

import { useAudit } from '../api/audit';
import { useOverview } from '../api/clusters';
import { JsonBlock } from '../components/JsonBlock';
import { PageHeader } from '../components/PageHeader';
import { PageLoader, QueryError } from '../components/QueryState';
import { RelativeTime } from '../components/RelativeTime';
import { hasContent } from '../lib/messages';

const LIMITS = ['100', '200', '500', '1000'];

export default function AuditPage() {
  const { data: clusters } = useOverview();
  const [clusterId, setClusterId] = useState<string | null>(null);
  const [limit, setLimit] = useState('200');
  const [search, setSearch] = useState('');
  const [expanded, setExpanded] = useState<number | null>(null);
  const audit = useAudit(clusterId ?? undefined, Number(limit));

  const clusterName = (id: string) => clusters?.find((c) => c.cluster.id === id)?.cluster.name ?? id;

  const entries = useMemo(() => {
    const q = search.trim().toLowerCase();
    return (audit.data ?? []).filter(
      (e) =>
        !q ||
        e.actor.toLowerCase().includes(q) ||
        e.action.toLowerCase().includes(q) ||
        e.target.toLowerCase().includes(q) ||
        e.result.toLowerCase().includes(q),
    );
  }, [audit.data, search]);

  return (
    <>
      <PageHeader title="Audit" description="Every change made through Brokerium, newest first" />
      <Group gap="sm" mb="md">
        <Select
          placeholder="All clusters"
          data={(clusters ?? []).map((c) => ({ value: c.cluster.id, label: c.cluster.name }))}
          value={clusterId}
          onChange={setClusterId}
          clearable
          searchable
          w={240}
        />
        <TextInput
          placeholder="Filter by actor, action, target or result"
          leftSection={<IconSearch size={14} />}
          value={search}
          onChange={(e) => setSearch(e.currentTarget.value)}
          w={320}
        />
        <Select
          data={LIMITS.map((l) => ({ value: l, label: `Last ${l}` }))}
          value={limit}
          onChange={(v) => setLimit(v ?? '200')}
          allowDeselect={false}
          w={130}
        />
      </Group>

      {audit.isPending && <PageLoader />}
      {audit.error && !audit.data && <QueryError error={audit.error} onRetry={() => void audit.refetch()} />}
      {audit.data && (
        <Paper withBorder radius="md">
          <Table.ScrollContainer minWidth={900}>
            <Table highlightOnHover verticalSpacing="xs">
              <Table.Thead>
                <Table.Tr>
                  <Table.Th w={32} />
                  <Table.Th>Time</Table.Th>
                  <Table.Th>Actor</Table.Th>
                  <Table.Th>Action</Table.Th>
                  <Table.Th>Cluster</Table.Th>
                  <Table.Th>Target</Table.Th>
                  <Table.Th>Result</Table.Th>
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {entries.map((e) => {
                  const ok = e.result === '' || e.result === 'ok';
                  const withDetail = hasContent(e.detail);
                  const open = expanded === e.id;
                  return (
                    <Fragment key={e.id}>
                      <Table.Tr
                        style={{ cursor: withDetail ? 'pointer' : undefined }}
                        onClick={() => withDetail && setExpanded(open ? null : e.id)}
                      >
                        <Table.Td>
                          {withDetail && (open ? <IconChevronDown size={14} /> : <IconChevronRight size={14} />)}
                        </Table.Td>
                        <Table.Td>
                          <RelativeTime value={e.at} />
                        </Table.Td>
                        <Table.Td>
                          <Text size="sm" fw={500}>
                            {e.actor}
                          </Text>
                        </Table.Td>
                        <Table.Td>
                          <Code>{e.action}</Code>
                        </Table.Td>
                        <Table.Td>
                          {e.clusterId ? (
                            <Anchor
                              component={Link}
                              to={`/clusters/${encodeURIComponent(e.clusterId)}`}
                              size="sm"
                              onClick={(ev) => ev.stopPropagation()}
                            >
                              {clusterName(e.clusterId)}
                            </Anchor>
                          ) : (
                            <Text size="sm" c="dimmed">
                              —
                            </Text>
                          )}
                        </Table.Td>
                        <Table.Td maw={260}>
                          <Text size="sm" ff="monospace" truncate="end" title={e.target}>
                            {e.target || '—'}
                          </Text>
                        </Table.Td>
                        <Table.Td maw={320}>
                          {ok ? (
                            <Badge size="sm" color="green" variant="light">
                              ok
                            </Badge>
                          ) : (
                            <Tooltip label={e.result} multiline maw={420} withArrow>
                              <Text size="sm" c="red" lineClamp={1}>
                                {e.result}
                              </Text>
                            </Tooltip>
                          )}
                        </Table.Td>
                      </Table.Tr>
                      {open && (
                        <Table.Tr>
                          <Table.Td colSpan={7}>
                            <Box p="xs">
                              <JsonBlock value={e.detail} maxHeight={280} />
                            </Box>
                          </Table.Td>
                        </Table.Tr>
                      )}
                    </Fragment>
                  );
                })}
                {entries.length === 0 && (
                  <Table.Tr>
                    <Table.Td colSpan={7}>
                      <Text size="sm" c="dimmed" ta="center" py="md">
                        No audit entries.
                      </Text>
                    </Table.Td>
                  </Table.Tr>
                )}
              </Table.Tbody>
            </Table>
          </Table.ScrollContainer>
        </Paper>
      )}
    </>
  );
}
