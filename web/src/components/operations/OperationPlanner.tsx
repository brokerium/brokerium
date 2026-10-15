// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { Button, Code, Group, Modal, Stack, Text, TextInput } from '@mantine/core';
import { IconPlayerPlay } from '@tabler/icons-react';
import { type ReactNode, useCallback, useMemo, useState } from 'react';
import { useNavigate } from 'react-router';

import {
  operationTitle,
  useConfirmOperation,
  useCreateOperation,
  useDiscardOperation,
  useOperationTypes,
} from '../../api/operations';
import type { Capabilities, Operation } from '../../api/types';
import { usePermissions } from '../../hooks/usePermissions';
import { type OperationTypeName, operationAvailability, withRole } from '../../lib/capabilities';
import { IrreversibleBadge, OperationStatusBadge } from '../badges';
import { notifySuccess } from '../notify';
import { PlanPreview } from './PlanPreview';
import { type Planner, PlannerContext } from './plannerContext';

interface OperationPlannerProviderProps {
  clusterId: string;
  clusterName: string;
  capabilities: Capabilities;
  children: ReactNode;
}

/**
 * Plans operations for one cluster and shows the dry-run preview with Confirm and Discard.
 * Irreversible operations require typing the cluster name.
 */
export function OperationPlannerProvider({ clusterId, clusterName, capabilities, children }: OperationPlannerProviderProps) {
  const navigate = useNavigate();
  const { user } = usePermissions();
  const { data: types } = useOperationTypes();
  const { mutateAsync: createOperation } = useCreateOperation(clusterId);
  const confirm = useConfirmOperation();
  const discard = useDiscardOperation();
  const [operation, setOperation] = useState<Operation | null>(null);
  const [pendingKey, setPendingKey] = useState<string | null>(null);
  const [typed, setTyped] = useState('');

  const plan = useCallback<Planner['plan']>(
    async (type, params, options) => {
      setPendingKey(options?.key ?? type);
      try {
        const op = await createOperation({ type, params });
        setTyped('');
        setOperation(op);
        return true;
      } catch {
        // The mutation cache already notified the user.
        return false;
      } finally {
        setPendingKey(null);
      }
    },
    [createOperation],
  );

  const availability = useCallback(
    (type: OperationTypeName) => withRole(operationAvailability(type, capabilities, types), user?.role),
    [capabilities, types, user?.role],
  );

  const planner = useMemo<Planner>(() => ({ plan, pendingKey, availability }), [plan, pendingKey, availability]);

  const irreversible = Boolean(operation && (operation.irreversible || operation.plan?.irreversible));
  const ready = !irreversible || typed === clusterName;
  const title = operation ? operationTitle(operation.type, types) : '';

  const handleDiscard = () => {
    if (operation) discard.mutate({ id: operation.id, clusterId: operation.clusterId });
    setOperation(null);
  };

  const handleDetails = () => {
    if (!operation) return;
    const id = operation.id;
    setOperation(null);
    navigate(`/operations/${encodeURIComponent(id)}`);
  };

  const handleConfirm = () => {
    if (!operation || !ready) return;
    confirm.mutate(
      { id: operation.id, confirmName: irreversible ? clusterName : undefined },
      {
        onSuccess: (started) => {
          setOperation(null);
          notifySuccess('Operation started', title);
          navigate(`/operations/${encodeURIComponent(started.id)}`);
        },
      },
    );
  };

  return (
    <PlannerContext.Provider value={planner}>
      {children}
      <Modal
        opened={operation !== null}
        onClose={handleDiscard}
        size="xl"
        closeOnClickOutside={false}
        title={
          <Group gap="sm">
            <Text fw={700}>Plan: {title}</Text>
            {operation && <OperationStatusBadge status={operation.status} />}
            {irreversible && <IrreversibleBadge />}
          </Group>
        }
      >
        {operation && (
          <Stack>
            <Text size="sm" c="dimmed">
              Dry run for cluster <b>{clusterName}</b>. Nothing changes until you confirm. Every step runs behind the
              health gate and can be paused or cancelled.
            </Text>
            <PlanPreview plan={operation.plan} irreversible={irreversible} />
            {irreversible && (
              <TextInput
                label={
                  <>
                    Type the cluster name <Code>{clusterName}</Code> to confirm
                  </>
                }
                value={typed}
                onChange={(e) => setTyped(e.currentTarget.value)}
                autoComplete="off"
                spellCheck={false}
              />
            )}
            <Group justify="space-between" mt="sm">
              <Button variant="default" color="red" onClick={handleDiscard} loading={discard.isPending}>
                Discard
              </Button>
              <Group gap="xs">
                <Button variant="subtle" onClick={handleDetails}>
                  Open details
                </Button>
                <Button
                  color={irreversible ? 'red' : undefined}
                  leftSection={<IconPlayerPlay size={16} />}
                  disabled={!ready}
                  loading={confirm.isPending}
                  onClick={handleConfirm}
                >
                  Confirm and start
                </Button>
              </Group>
            </Group>
          </Stack>
        )}
      </Modal>
    </PlannerContext.Provider>
  );
}
