// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

import { Badge, Group, NumberInput, SegmentedControl, Stack, Switch, Text, TextInput } from '@mantine/core';
import { useState } from 'react';

import { useOperationTypes } from '../../api/operations';
import type { OperationParams, Snapshot } from '../../api/types';
import { usePlanner } from '../../components/operations/plannerContext';
import { useClusterContext } from '../../routes/cluster/clusterContext';
import { ParamsModal } from '../../routes/cluster/common';
import type { Problem, ProblemFix } from '../api/problems';
import { fixReady } from './problems';

const MiB = 1024 * 1024;

type RebalanceMode = NonNullable<OperationParams['rebalance']['mode']>;

const REBALANCE_MODES: { value: RebalanceMode; label: string }[] = [
  { value: 'even', label: 'Even' },
  { value: 'replicas', label: 'Replicas' },
  { value: 'leaders', label: 'Leaders' },
];

interface FixModalProps {
  problem: Problem;
  fix: ProblemFix;
  onClose: () => void;
}

/** Shows the parameters of a fix as the server suggests them and plans its operation. */
export function FixModal({ problem, fix, onClose }: FixModalProps) {
  const planner = usePlanner();
  const { snapshot } = useClusterContext();
  const { data: types } = useOperationTypes();
  const [draft, setDraft] = useState<ProblemFix>(fix);
  const key = `fix-${problem.fingerprint}`;
  return (
    <ParamsModal
      opened
      onClose={onClose}
      title={fix.label}
      description={types?.find((t) => t.type === fix.operation)?.description}
      planning={planner.pendingKey === key}
      disabled={!fixReady(draft)}
      onPlan={() => planner.planNamed(draft.operation, draft.params, { key })}
    >
      <FixFields draft={draft} onChange={setDraft} snapshot={snapshot} />
    </ParamsModal>
  );
}

interface FixFieldsProps {
  draft: ProblemFix;
  onChange: (draft: ProblemFix) => void;
  snapshot: Snapshot | undefined;
}

function FixFields({ draft, onChange, snapshot }: FixFieldsProps) {
  switch (draft.operation) {
    case 'change-replication-factor': {
      const { params } = draft;
      const brokers = snapshot?.brokers ?? [];
      const serving = brokers.filter((b) => b.alive && !b.fenced).length;
      const racks = brokers.length > 0 && brokers.every((b) => b.rack !== '');
      return (
        <Stack gap="sm">
          <Topics topics={params.topics} />
          <NumberInput
            label="Replication factor"
            description={`${serving} broker(s) serve`}
            min={1}
            max={serving || undefined}
            allowDecimal={false}
            value={params.replicationFactor}
            onChange={(v) => onChange({ ...draft, params: { ...params, replicationFactor: Number(v) || 0 } })}
          />
          <Throttle
            value={params.throttleBytesPerSec}
            onChange={(t) => onChange({ ...draft, params: { ...params, throttleBytesPerSec: t } })}
          />
          <Switch
            label="Spread the replicas of a partition over racks"
            checked={params.rackAware ?? racks}
            disabled={!racks}
            onChange={(e) => onChange({ ...draft, params: { ...params, rackAware: e.currentTarget.checked } })}
          />
        </Stack>
      );
    }
    case 'alter-topic-configs': {
      const { params } = draft;
      const removed = params.delete ?? [];
      return (
        <Stack gap="sm">
          <Topics topics={params.topics} />
          {Object.entries(params.set ?? {}).map(([name, value]) => (
            <TextInput
              key={name}
              label={name}
              value={value}
              onChange={(e) =>
                onChange({ ...draft, params: { ...params, set: { ...params.set, [name]: e.currentTarget.value } } })
              }
            />
          ))}
          {removed.length > 0 && <Text size="sm">Removes {removed.join(', ')}, so the broker defaults apply.</Text>}
        </Stack>
      );
    }
    case 'align-broker-configs': {
      const { params } = draft;
      return (
        <Stack gap="sm">
          {params.keys.map((name) => (
            <TextInput
              key={name}
              label={name}
              placeholder="The value most brokers have"
              value={params.values?.[name] ?? ''}
              onChange={(e) => {
                const values = { ...params.values };
                const value = e.currentTarget.value;
                if (value) values[name] = value;
                else delete values[name];
                onChange({ ...draft, params: { ...params, values } });
              }}
            />
          ))}
        </Stack>
      );
    }
    case 'rebalance': {
      const { params } = draft;
      return (
        <Stack gap="sm">
          <SegmentedControl
            data={REBALANCE_MODES}
            value={params.mode ?? 'even'}
            onChange={(v) => onChange({ ...draft, params: { ...params, mode: v as RebalanceMode } })}
          />
          <Throttle
            value={params.throttleBytesPerSec}
            onChange={(t) => onChange({ ...draft, params: { ...params, throttleBytesPerSec: t } })}
          />
        </Stack>
      );
    }
    case 'finalize-upgrade':
      return (
        <Text size="sm">Raises metadata.version and the other features to the highest level that all nodes support.</Text>
      );
    default: {
      const exhaustive: never = draft;
      return exhaustive;
    }
  }
}

function Topics({ topics }: { topics: string[] }) {
  return (
    <Group gap={6}>
      <Text size="sm" fw={500}>
        {topics.length === 1 ? 'Topic' : 'Topics'}
      </Text>
      {topics.map((t) => (
        <Badge key={t} variant="light" color="gray" tt="none" ff="monospace">
          {t}
        </Badge>
      ))}
    </Group>
  );
}

function Throttle({ value, onChange }: { value: number | undefined; onChange: (bytesPerSec: number | undefined) => void }) {
  return (
    <NumberInput
      label="Throttle (MiB/s)"
      description="Replication rate of the moved replicas; 50 MiB/s when empty"
      min={0.1}
      placeholder="50"
      value={value === undefined ? '' : value / MiB}
      onChange={(v) => onChange(v === '' ? undefined : Math.round(Number(v) * MiB))}
    />
  );
}
