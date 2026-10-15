// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import type { Broker, BrokerLoad, DistributionStats, Partition, Topic } from '../api/types';
import { percentOf } from './format';

export function isUnderReplicated(p: Partition): boolean {
  return p.isr.length < p.replicas.length;
}

export function isOffline(p: Partition): boolean {
  return p.leader < 0;
}

export function underReplicatedCount(topic: Topic): number {
  return topic.partitions.filter(isUnderReplicated).length;
}

export function offlineCount(topic: Topic): number {
  return topic.partitions.filter(isOffline).length;
}

/** Replicas of a partition that are not in the ISR. */
export function outOfSyncReplicas(p: Partition): number[] {
  const isr = new Set(p.isr);
  return p.replicas.filter((id) => !isr.has(id));
}

/** Disk usage of a broker in percent; null when the broker does not report disk sizes. */
export function diskUsedPercent(broker: Broker): number | null {
  return percentOf(broker.diskTotalBytes - broker.diskUsableBytes, broker.diskTotalBytes);
}

export interface BrokerCell {
  replicas: number;
  leaders: number;
}

export interface TopicDistributionRow {
  topic: string;
  internal: boolean;
  partitions: number;
  cells: Record<number, BrokerCell>;
}

/** Counts replicas and leaders of every topic on every broker (topics x brokers matrix). */
export function topicBrokerMatrix(topics: Topic[], brokerIds: number[]): TopicDistributionRow[] {
  return topics
    .map((topic) => {
      const cells: Record<number, BrokerCell> = {};
      for (const id of brokerIds) cells[id] = { replicas: 0, leaders: 0 };
      for (const p of topic.partitions) {
        for (const id of p.replicas) {
          cells[id] ??= { replicas: 0, leaders: 0 };
          cells[id].replicas += 1;
        }
        if (p.leader >= 0) {
          cells[p.leader] ??= { replicas: 0, leaders: 0 };
          cells[p.leader].leaders += 1;
        }
      }
      return { topic: topic.name, internal: topic.internal, partitions: topic.partitions.length, cells };
    })
    .sort((a, b) => a.topic.localeCompare(b.topic));
}

export function maxReplicasPerCell(rows: TopicDistributionRow[]): number {
  let max = 0;
  for (const row of rows) {
    for (const cell of Object.values(row.cells)) max = Math.max(max, cell.replicas);
  }
  return max;
}

/** Heat level from 0 (empty) to 9 (the busiest cell), used to pick a color shade. */
export function heatLevel(value: number, max: number): number {
  if (value <= 0 || max <= 0) return 0;
  return Math.max(1, Math.min(9, Math.ceil((value / max) * 9)));
}

export interface BrokerComparisonRow {
  id: number;
  rack: string;
  excluded: boolean;
  before?: BrokerLoad;
  after?: BrokerLoad;
}

/** Joins before/after broker loads of a plan by broker id. */
export function compareDistributions(before?: DistributionStats, after?: DistributionStats): BrokerComparisonRow[] {
  const rows = new Map<number, BrokerComparisonRow>();
  for (const load of before?.brokers ?? []) {
    rows.set(load.id, { id: load.id, rack: load.rack, excluded: load.excluded, before: load });
  }
  for (const load of after?.brokers ?? []) {
    const row = rows.get(load.id) ?? { id: load.id, rack: load.rack, excluded: load.excluded };
    row.after = load;
    row.excluded = row.excluded || load.excluded;
    rows.set(load.id, row);
  }
  return [...rows.values()].sort((a, b) => a.id - b.id);
}

export interface MoveSummary {
  partitions: number;
  replicaMoves: number;
  leaderChanges: number;
  bytes: number;
}

export function summarizeMoves(moves: { replicaMoves: number; leaderChange: boolean; sizeBytes: number }[]): MoveSummary {
  return moves.reduce<MoveSummary>(
    (acc, m) => ({
      partitions: acc.partitions + 1,
      replicaMoves: acc.replicaMoves + m.replicaMoves,
      leaderChanges: acc.leaderChanges + (m.leaderChange ? 1 : 0),
      bytes: acc.bytes + m.sizeBytes * m.replicaMoves,
    }),
    { partitions: 0, replicaMoves: 0, leaderChanges: 0, bytes: 0 },
  );
}
