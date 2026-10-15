// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { Badge, Group, NumberInput, Paper, Table, Text } from '@mantine/core';
import { IconArrowsVertical } from '@tabler/icons-react';
import { useState } from 'react';

import { type NodePool, useNodePools } from '../../api/nodes';
import { GatedButton } from '../../components/Gated';
import { usePlanner } from '../../components/operations/plannerContext';
import { PageLoader, QueryError } from '../../components/QueryState';
import { useClusterContext } from './clusterContext';
import { ParamsModal, Section } from './common';

/** Strimzi node pools of the cluster, with scaling of broker pools. */
export function NodePoolsSection() {
  const { clusterId } = useClusterContext();
  const planner = usePlanner();
  const pools = useNodePools(clusterId, true);
  const [scaling, setScaling] = useState<NodePool | null>(null);
  const [replicas, setReplicas] = useState(0);

  const open = (pool: NodePool) => {
    setReplicas(pool.replicas);
    setScaling(pool);
  };

  return (
    <Section
      title="Node pools"
      description="Strimzi KafkaNodePools. Scaling a broker pool rebalances onto new brokers or empties brokers before the operator removes them."
    >
      {pools.isPending ? (
        <PageLoader />
      ) : pools.isError ? (
        <QueryError error={pools.error} onRetry={() => void pools.refetch()} />
      ) : (
        <Paper withBorder radius="md">
            <Table verticalSpacing="xs">
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>Pool</Table.Th>
                  <Table.Th>Roles</Table.Th>
                  <Table.Th ta="right">Replicas</Table.Th>
                  <Table.Th>Node ids</Table.Th>
                  <Table.Th />
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {pools.data.map((p) => {
                  const controllers = p.roles.includes('controller');
                  return (
                    <Table.Tr key={p.name}>
                      <Table.Td>
                        <Text size="sm" ff="monospace">
                          {p.name}
                        </Text>
                      </Table.Td>
                      <Table.Td>
                        <Group gap={4}>
                          {p.roles.map((r) => (
                            <Badge key={r} size="sm" variant="light" color={r === 'controller' ? 'grape' : 'indigo'}>
                              {r}
                            </Badge>
                          ))}
                        </Group>
                      </Table.Td>
                      <Table.Td ta="right">
                        {p.readyReplicas}/{p.replicas}
                        {!p.settled && (
                          <Badge ml={6} size="xs" color="yellow" variant="light">
                            reconciling
                          </Badge>
                        )}
                      </Table.Td>
                      <Table.Td>
                        <Text size="sm" ff="monospace">
                          {(p.nodeIds ?? []).join(', ') || '—'}
                        </Text>
                      </Table.Td>
                      <Table.Td ta="right">
                        <GatedButton
                          size="xs"
                          variant="default"
                          leftSection={<IconArrowsVertical size={14} />}
                          availability={
                            controllers
                              ? { allowed: false, reason: 'Strimzi does not support dynamic controller quorums yet' }
                              : planner.availability('scale-node-pool')
                          }
                          onClick={() => open(p)}
                        >
                          Scale
                        </GatedButton>
                      </Table.Td>
                    </Table.Tr>
                  );
                })}
              </Table.Tbody>
            </Table>
          </Paper>
      )}
      <ParamsModal
        opened={scaling !== null}
        onClose={() => setScaling(null)}
        title={`Scale node pool ${scaling?.name ?? ''}`}
        description="Nothing changes until you confirm the plan."
        planning={planner.pendingKey === 'scale-node-pool'}
        disabled={scaling === null || replicas === scaling.replicas}
        onPlan={() => planner.plan('scale-node-pool', { pool: scaling?.name ?? '', replicas })}
      >
        <NumberInput label="Brokers" min={1} value={replicas} onChange={(v) => setReplicas(Number(v) || 1)} />
      </ParamsModal>
    </Section>
  );
}
