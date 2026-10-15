// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { ActionIcon, Badge, Button, Group, Modal, Select, SimpleGrid, Stack, Table, Text, TextInput, Tooltip } from '@mantine/core';
import { IconPlus, IconSearch, IconTrash } from '@tabler/icons-react';
import { useMemo, useState } from 'react';

import { useAcls, useCreateAcl, useDeleteAcl } from '../../../api/security';
import type { Acl } from '../../../api/types';
import { confirmAction } from '../../../components/confirm';
import { GatedButton } from '../../../components/Gated';
import { notifySuccess } from '../../../components/notify';
import { PageLoader, QueryError } from '../../../components/QueryState';
import type { Availability } from '../../../lib/capabilities';
import { principalError } from '../../../lib/validation';

const RESOURCE_TYPES: Acl['resourceType'][] = ['TOPIC', 'GROUP', 'CLUSTER', 'TRANSACTIONAL_ID', 'DELEGATION_TOKEN', 'USER'];
const OPERATIONS = [
  'ALL',
  'READ',
  'WRITE',
  'CREATE',
  'DELETE',
  'ALTER',
  'DESCRIBE',
  'CLUSTER_ACTION',
  'DESCRIBE_CONFIGS',
  'ALTER_CONFIGS',
  'IDEMPOTENT_WRITE',
  'CREATE_TOKENS',
  'DESCRIBE_TOKENS',
];

const EMPTY_ACL: Acl = {
  resourceType: 'TOPIC',
  resourceName: '',
  patternType: 'LITERAL',
  principal: 'User:',
  host: '*',
  operation: 'READ',
  permission: 'ALLOW',
};

function aclKey(a: Acl): string {
  return [a.principal, a.host, a.resourceType, a.patternType, a.resourceName, a.operation, a.permission].join('|');
}

export function AclsPanel({ clusterId, editable }: { clusterId: string; editable: Availability }) {
  const acls = useAcls(clusterId);
  const create = useCreateAcl(clusterId);
  const remove = useDeleteAcl(clusterId);
  const [search, setSearch] = useState('');
  const [draft, setDraft] = useState<Acl | null>(null);
  const [submitted, setSubmitted] = useState(false);

  const rows = useMemo(() => {
    const q = search.trim().toLowerCase();
    return (acls.data ?? [])
      .filter((a) => !q || aclKey(a).toLowerCase().includes(q))
      .sort((a, b) => a.principal.localeCompare(b.principal) || a.resourceName.localeCompare(b.resourceName));
  }, [acls.data, search]);

  if (acls.isPending) return <PageLoader />;
  if (acls.error) return <QueryError error={acls.error} onRetry={() => void acls.refetch()} />;

  const del = async (acl: Acl) => {
    const ok = await confirmAction({
      title: 'Delete ACL',
      message: `${acl.permission} ${acl.operation} on ${acl.resourceType} ${acl.patternType === 'PREFIXED' ? 'prefix ' : ''}"${acl.resourceName}" for ${acl.principal} from ${acl.host}. Clients relying on it lose access immediately.`,
      confirmLabel: 'Delete ACL',
    });
    if (ok) remove.mutate(acl, { onSuccess: () => notifySuccess('ACL deleted') });
  };

  const principalErr = submitted && draft ? principalError(draft.principal) : null;
  const nameErr = submitted && draft && draft.resourceType !== 'CLUSTER' && !draft.resourceName.trim() ? 'Required' : null;

  const save = () => {
    setSubmitted(true);
    if (!draft || principalError(draft.principal)) return;
    const acl: Acl = {
      ...draft,
      principal: draft.principal.trim(),
      host: draft.host.trim() || '*',
      resourceName: draft.resourceType === 'CLUSTER' ? 'kafka-cluster' : draft.resourceName.trim(),
    };
    if (!acl.resourceName) return;
    create.mutate(acl, {
      onSuccess: () => {
        notifySuccess('ACL created', `${acl.permission} ${acl.operation} for ${acl.principal}`);
        setDraft(null);
      },
    });
  };

  return (
    <Stack gap="sm">
      <Group justify="space-between">
        <TextInput
          placeholder="Filter by principal, resource or operation"
          leftSection={<IconSearch size={14} />}
          value={search}
          onChange={(e) => setSearch(e.currentTarget.value)}
          w={340}
        />
        <GatedButton
          availability={editable}
          leftSection={<IconPlus size={16} />}
          onClick={() => {
            setSubmitted(false);
            setDraft({ ...EMPTY_ACL });
          }}
        >
          Create ACL
        </GatedButton>
      </Group>
      <Table.ScrollContainer minWidth={900}>
        <Table striped highlightOnHover verticalSpacing="xs">
          <Table.Thead>
            <Table.Tr>
              <Table.Th>Principal</Table.Th>
              <Table.Th>Host</Table.Th>
              <Table.Th>Resource</Table.Th>
              <Table.Th>Pattern</Table.Th>
              <Table.Th>Operation</Table.Th>
              <Table.Th>Permission</Table.Th>
              <Table.Th w={48} />
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {rows.map((a) => (
              <Table.Tr key={aclKey(a)}>
                <Table.Td>
                  <Text size="sm" ff="monospace">
                    {a.principal}
                  </Text>
                </Table.Td>
                <Table.Td>
                  <Text size="sm" ff="monospace">
                    {a.host}
                  </Text>
                </Table.Td>
                <Table.Td>
                  <Group gap={6} wrap="nowrap">
                    <Badge size="xs" variant="outline" color="gray">
                      {a.resourceType}
                    </Badge>
                    <Text size="sm" ff="monospace">
                      {a.resourceName}
                    </Text>
                  </Group>
                </Table.Td>
                <Table.Td>
                  <Text size="sm">{a.patternType.toLowerCase()}</Text>
                </Table.Td>
                <Table.Td>
                  <Text size="sm">{a.operation}</Text>
                </Table.Td>
                <Table.Td>
                  <Badge size="sm" color={a.permission === 'ALLOW' ? 'green' : 'red'} variant="light">
                    {a.permission}
                  </Badge>
                </Table.Td>
                <Table.Td>
                  <Tooltip label={editable.allowed ? 'Delete' : editable.reason} withArrow>
                    <ActionIcon
                      variant="subtle"
                      color="red"
                      disabled={!editable.allowed}
                      onClick={() => void del(a)}
                      aria-label="Delete ACL"
                    >
                      <IconTrash size={14} />
                    </ActionIcon>
                  </Tooltip>
                </Table.Td>
              </Table.Tr>
            ))}
            {rows.length === 0 && (
              <Table.Tr>
                <Table.Td colSpan={7}>
                  <Text size="sm" c="dimmed" ta="center" py="md">
                    No ACLs.
                  </Text>
                </Table.Td>
              </Table.Tr>
            )}
          </Table.Tbody>
        </Table>
      </Table.ScrollContainer>

      <Modal opened={draft !== null} onClose={() => setDraft(null)} title={<Text fw={600}>Create ACL</Text>} size="lg">
        {draft && (
          <form
            onSubmit={(e) => {
              e.preventDefault();
              save();
            }}
          >
            <Stack>
              <SimpleGrid cols={2}>
                <TextInput
                  label="Principal"
                  placeholder="User:alice"
                  value={draft.principal}
                  onChange={(e) => setDraft({ ...draft, principal: e.currentTarget.value })}
                  error={principalErr}
                  data-autofocus
                />
                <TextInput
                  label="Host"
                  description="* for any host"
                  value={draft.host}
                  onChange={(e) => setDraft({ ...draft, host: e.currentTarget.value })}
                />
              </SimpleGrid>
              <SimpleGrid cols={2}>
                <Select
                  label="Resource type"
                  data={RESOURCE_TYPES}
                  value={draft.resourceType}
                  onChange={(v) => v && setDraft({ ...draft, resourceType: v as Acl['resourceType'] })}
                  allowDeselect={false}
                />
                <Select
                  label="Pattern type"
                  data={[
                    { value: 'LITERAL', label: 'Literal' },
                    { value: 'PREFIXED', label: 'Prefixed' },
                  ]}
                  value={draft.patternType}
                  onChange={(v) => v && setDraft({ ...draft, patternType: v as Acl['patternType'] })}
                  allowDeselect={false}
                />
              </SimpleGrid>
              <TextInput
                label="Resource name"
                description={draft.resourceType === 'CLUSTER' ? 'Always kafka-cluster' : 'Use * for all resources of the type'}
                value={draft.resourceType === 'CLUSTER' ? 'kafka-cluster' : draft.resourceName}
                disabled={draft.resourceType === 'CLUSTER'}
                onChange={(e) => setDraft({ ...draft, resourceName: e.currentTarget.value })}
                error={nameErr}
              />
              <SimpleGrid cols={2}>
                <Select
                  label="Operation"
                  data={OPERATIONS}
                  value={draft.operation}
                  onChange={(v) => v && setDraft({ ...draft, operation: v })}
                  searchable
                  allowDeselect={false}
                />
                <Select
                  label="Permission"
                  data={['ALLOW', 'DENY']}
                  value={draft.permission}
                  onChange={(v) => v && setDraft({ ...draft, permission: v as Acl['permission'] })}
                  allowDeselect={false}
                />
              </SimpleGrid>
              <Group justify="flex-end">
                <Button variant="default" onClick={() => setDraft(null)}>
                  Cancel
                </Button>
                <Button type="submit" loading={create.isPending}>
                  Create ACL
                </Button>
              </Group>
            </Stack>
          </form>
        )}
      </Modal>
    </Stack>
  );
}
