// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import {
  Accordion,
  Alert,
  Anchor,
  Badge,
  Button,
  Code,
  DataList,
  Group,
  Paper,
  SimpleGrid,
  Stack,
  Text,
  TextInput,
  Title,
} from '@mantine/core';
import {
  IconAlertTriangle,
  IconCircleX,
  IconPlayerPause,
  IconPlayerPlay,
  IconPlayerStop,
  IconTrash,
} from '@tabler/icons-react';
import { useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router';

import { useOverview } from '../api/clusters';
import { isApiError } from '../api/errors';
import {
  operationTitle,
  useConfirmOperation,
  useControlOperation,
  useDiscardOperation,
  useOperation,
  useOperationTypes,
} from '../api/operations';
import type { Operation } from '../api/types';
import { IrreversibleBadge, OperationStatusBadge } from '../components/badges';
import { confirmAction } from '../components/confirm';
import { CopyText } from '../components/CopyText';
import { JsonBlock } from '../components/JsonBlock';
import { LiveDuration } from '../components/LiveDuration';
import { notifySuccess } from '../components/notify';
import { PlanPreview } from '../components/operations/PlanPreview';
import { StepsTimeline } from '../components/operations/StepsTimeline';
import { PageHeader } from '../components/PageHeader';
import { PageLoader, QueryError } from '../components/QueryState';
import { RelativeTime } from '../components/RelativeTime';
import { usePermissions } from '../hooks/usePermissions';
import { formatDateTime } from '../lib/format';
import { hasContent } from '../lib/messages';
import { isOperationActive } from '../lib/status';

function PlannedControls({ op, clusterName }: { op: Operation; clusterName: string }) {
  const navigate = useNavigate();
  const confirm = useConfirmOperation();
  const discard = useDiscardOperation();
  const [typed, setTyped] = useState('');
  const irreversible = op.irreversible || op.plan?.irreversible;
  const ready = !irreversible || typed === clusterName;

  const onDiscard = async () => {
    const ok = await confirmAction({
      title: 'Discard plan',
      message: 'The planned operation is deleted. Nothing has been changed on the cluster.',
      confirmLabel: 'Discard',
    });
    if (!ok) return;
    discard.mutate(op, {
      onSuccess: () => {
        notifySuccess('Plan discarded');
        navigate(`/clusters/${encodeURIComponent(op.clusterId)}/operations`, { replace: true });
      },
    });
  };

  return (
    <Paper withBorder radius="md" p="md">
      <Stack gap="sm">
        <Text size="sm">
          This is a dry-run plan. Nothing changes on the cluster until you confirm it; every step then runs behind the
          health gate and can be paused or cancelled.
        </Text>
        {irreversible && (
          <TextInput
            label={
              <>
                Irreversible: type the cluster name <Code>{clusterName}</Code> to confirm
              </>
            }
            value={typed}
            onChange={(e) => setTyped(e.currentTarget.value)}
            autoComplete="off"
            spellCheck={false}
            maw={420}
          />
        )}
        <Group gap="xs">
          <Button
            color={irreversible ? 'red' : undefined}
            leftSection={<IconPlayerPlay size={16} />}
            disabled={!ready}
            loading={confirm.isPending}
            onClick={() =>
              confirm.mutate(
                { id: op.id, confirmName: irreversible ? clusterName : undefined },
                { onSuccess: () => notifySuccess('Operation started') },
              )
            }
          >
            Confirm and start
          </Button>
          <Button
            variant="default"
            color="red"
            leftSection={<IconTrash size={16} />}
            loading={discard.isPending}
            onClick={() => void onDiscard()}
          >
            Discard
          </Button>
        </Group>
      </Stack>
    </Paper>
  );
}

function RunControls({ op }: { op: Operation }) {
  const control = useControlOperation();
  const pending = (action: string) => control.isPending && control.variables?.action === action;

  const cancel = async () => {
    const ok = await confirmAction({
      title: 'Cancel operation',
      message:
        'The operation stops after the current step. Cleanup steps (for example removing replication throttles) still run. Work already done is not rolled back.',
      confirmLabel: 'Cancel operation',
    });
    if (ok) control.mutate({ id: op.id, action: 'cancel' });
  };

  return (
    <Group gap="xs">
      {op.status === 'running' && (
        <Button
          variant="light"
          color="yellow"
          leftSection={<IconPlayerPause size={16} />}
          loading={pending('pause')}
          onClick={() => control.mutate({ id: op.id, action: 'pause' })}
        >
          Pause
        </Button>
      )}
      {op.status === 'paused' && (
        <Button leftSection={<IconPlayerPlay size={16} />} loading={pending('resume')} onClick={() => control.mutate({ id: op.id, action: 'resume' })}>
          Resume
        </Button>
      )}
      <Button
        variant="light"
        color="red"
        leftSection={<IconPlayerStop size={16} />}
        loading={pending('cancel')}
        onClick={() => void cancel()}
      >
        Cancel
      </Button>
      {op.requestedAction && (
        <Badge variant="outline" color="yellow">
          {op.requestedAction} requested
        </Badge>
      )}
    </Group>
  );
}

export default function OperationPage() {
  const { operationId = '' } = useParams();
  const operation = useOperation(operationId);
  const { data: types } = useOperationTypes();
  const { data: clusters } = useOverview();
  const { canOperate } = usePermissions();

  if (operation.isPending) return <PageLoader label="Loading operation" />;
  if (!operation.data) {
    return (
      <QueryError
        error={operation.error}
        title={isApiError(operation.error, 404) ? 'Operation not found' : 'Could not load the operation'}
        onRetry={() => void operation.refetch()}
      />
    );
  }

  const op = operation.data;
  const clusterName = clusters?.find((c) => c.cluster.id === op.clusterId)?.cluster.name ?? op.clusterId;
  const title = operationTitle(op.type, types);
  const irreversible = op.irreversible || Boolean(op.plan?.irreversible);
  const warnings = op.plan?.warnings ?? [];
  const steps = op.steps ?? [];
  const clusterPath = `/clusters/${encodeURIComponent(op.clusterId)}`;

  return (
    <Stack gap="md">
      <PageHeader
        crumbs={[
          { label: 'Operations', to: '/operations' },
          { label: clusterName, to: `${clusterPath}/operations` },
          { label: title },
        ]}
        title={title}
        badges={
          <Group gap={6}>
            <OperationStatusBadge status={op.status} size="lg" />
            {irreversible && <IrreversibleBadge size="lg" />}
          </Group>
        }
        description={op.summary || op.plan?.summary}
      />

      <Paper withBorder radius="md" p="md">
        <SimpleGrid cols={{ base: 1, sm: 2, lg: 4 }} spacing="md">
          <DataList orientation="vertical" size="sm">
            <DataList.Item>
              <DataList.ItemLabel>Cluster</DataList.ItemLabel>
              <DataList.ItemValue>
                <Anchor component={Link} to={clusterPath} size="sm">
                  {clusterName}
                </Anchor>
              </DataList.ItemValue>
            </DataList.Item>
          </DataList>
          <DataList orientation="vertical" size="sm">
            <DataList.Item>
              <DataList.ItemLabel>Created</DataList.ItemLabel>
              <DataList.ItemValue>
                <Text size="sm">
                  {op.createdBy || 'system'}, <RelativeTime value={op.createdAt} />
                </Text>
              </DataList.ItemValue>
            </DataList.Item>
          </DataList>
          <DataList orientation="vertical" size="sm">
            <DataList.Item>
              <DataList.ItemLabel>Started</DataList.ItemLabel>
              <DataList.ItemValue>
                <Text size="sm">{op.startedAt ? formatDateTime(op.startedAt) : 'not started'}</Text>
              </DataList.ItemValue>
            </DataList.Item>
          </DataList>
          <DataList orientation="vertical" size="sm">
            <DataList.Item>
              <DataList.ItemLabel>{op.finishedAt ? 'Finished' : 'Duration'}</DataList.ItemLabel>
              <DataList.ItemValue>
                <Text size="sm">
                  {op.finishedAt && `${formatDateTime(op.finishedAt)} · `}
                  {op.startedAt ? <LiveDuration start={op.startedAt} end={op.finishedAt} /> : '—'}
                </Text>
              </DataList.ItemValue>
            </DataList.Item>
          </DataList>
        </SimpleGrid>
        <Group gap={4} mt="sm">
          <Text size="xs" c="dimmed">
            Operation ID
          </Text>
          <CopyText value={op.id} size="xs" c="dimmed" />
        </Group>
      </Paper>

      {op.error && (
        <Alert color="red" icon={<IconCircleX size={18} />} title="Operation failed">
          <Text size="sm" style={{ whiteSpace: 'pre-wrap' }}>
            {op.error}
          </Text>
        </Alert>
      )}
      {op.status !== 'planned' &&
        warnings.map((w) => (
          <Alert key={w} color="yellow" icon={<IconAlertTriangle size={18} />} variant="light">
            {w}
          </Alert>
        ))}

      {op.status === 'planned' && canOperate && <PlannedControls op={op} clusterName={clusterName} />}
      {isOperationActive(op.status) && canOperate && <RunControls op={op} />}

      {op.status === 'planned' ? (
        <Paper withBorder radius="md" p="md">
          <Title order={4} mb="sm">
            Plan
          </Title>
          <PlanPreview plan={op.plan} irreversible={irreversible} />
        </Paper>
      ) : (
        <Accordion variant="contained" radius="md">
          <Accordion.Item value="plan">
            <Accordion.Control>Plan</Accordion.Control>
            <Accordion.Panel>
              <PlanPreview plan={op.plan} irreversible={irreversible} />
            </Accordion.Panel>
          </Accordion.Item>
          {hasContent(op.params) && (
            <Accordion.Item value="params">
              <Accordion.Control>Parameters</Accordion.Control>
              <Accordion.Panel>
                <JsonBlock value={op.params} />
              </Accordion.Panel>
            </Accordion.Item>
          )}
        </Accordion>
      )}

      {steps.length > 0 && (
        <Paper withBorder radius="md" p="md">
          <Group justify="space-between" mb="md">
            <Title order={4}>Steps</Title>
            {isOperationActive(op.status) && (
              <Text size="xs" c="dimmed">
                Updating every 2 s
              </Text>
            )}
          </Group>
          <StepsTimeline steps={steps} />
        </Paper>
      )}
    </Stack>
  );
}
