// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { Badge, Group, Tooltip } from '@mantine/core';

interface ReplicaListProps {
  replicas: number[];
  /** In-sync replicas; replicas outside this set are highlighted as missing. */
  isr?: number[];
  offline?: number[] | null;
  leader?: number;
  /** Replicas that are new compared to another assignment (plan previews). */
  added?: number[];
}

/** Broker ids of a partition assignment; the leader is filled, missing replicas are red. */
export function ReplicaList({ replicas, isr, offline, leader, added }: ReplicaListProps) {
  const inSync = isr ? new Set(isr) : null;
  const down = new Set(offline ?? []);
  const fresh = new Set(added ?? []);
  return (
    <Group gap={4} wrap="nowrap">
      {replicas.map((id) => {
        const missing = (inSync !== null && !inSync.has(id)) || down.has(id);
        const isLeader = leader === id;
        const color = missing ? 'red' : fresh.has(id) ? 'teal' : 'indigo';
        const hint = [isLeader && 'leader', missing && (down.has(id) ? 'offline' : 'not in ISR'), fresh.has(id) && 'new replica']
          .filter(Boolean)
          .join(', ');
        return (
          <Tooltip key={id} label={hint || 'replica'} disabled={!hint} withArrow>
            <Badge size="sm" radius="sm" color={color} variant={isLeader ? 'filled' : 'light'} style={{ fontVariantNumeric: 'tabular-nums' }}>
              {id}
            </Badge>
          </Tooltip>
        );
      })}
    </Group>
  );
}
