// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { describe, expect, it } from 'vitest';

import type { Broker, Partition, Topic } from '../api/types';
import {
  compareDistributions,
  diskUsedPercent,
  heatLevel,
  maxReplicasPerCell,
  outOfSyncReplicas,
  summarizeMoves,
  topicBrokerMatrix,
  underReplicatedCount,
} from './distribution';

function partition(id: number, leader: number, replicas: number[], isr: number[] = replicas): Partition {
  return { id, leader, leaderEpoch: 0, replicas, isr, sizeBytes: 0 };
}

function topic(name: string, partitions: Partition[]): Topic {
  return {
    name,
    id: name,
    internal: false,
    replicationFactor: partitions[0]?.replicas.length ?? 0,
    minInsyncReplicas: 1,
    configs: {},
    sizeBytes: 0,
    partitions,
  };
}

describe('partition health', () => {
  it('counts under-replicated partitions and lists missing replicas', () => {
    const t = topic('orders', [partition(0, 101, [101, 102, 103], [101, 103]), partition(1, 102, [102, 103, 101])]);
    expect(underReplicatedCount(t)).toBe(1);
    expect(outOfSyncReplicas(t.partitions[0])).toEqual([102]);
  });
});

describe('topicBrokerMatrix', () => {
  it('counts replicas and leaders per broker', () => {
    const rows = topicBrokerMatrix(
      [
        topic('b-topic', [partition(0, 101, [101, 102]), partition(1, -1, [102, 103])]),
        topic('a-topic', [partition(0, 103, [103, 101])]),
      ],
      [101, 102, 103],
    );
    expect(rows.map((r) => r.topic)).toEqual(['a-topic', 'b-topic']);
    expect(rows[1].cells).toEqual({
      101: { replicas: 1, leaders: 1 },
      102: { replicas: 2, leaders: 0 },
      103: { replicas: 1, leaders: 0 },
    });
    expect(maxReplicasPerCell(rows)).toBe(2);
  });

  it('maps values to heat levels', () => {
    expect(heatLevel(0, 10)).toBe(0);
    expect(heatLevel(1, 10)).toBe(1);
    expect(heatLevel(10, 10)).toBe(9);
    expect(heatLevel(5, 0)).toBe(0);
  });
});

describe('plan helpers', () => {
  it('joins before and after loads by broker id', () => {
    const load = (id: number, replicas: number, excluded = false) => ({
      id,
      rack: '',
      replicas,
      leaders: 0,
      dataBytes: 0,
      bytesInPerSec: 0,
      excluded,
    });
    const rows = compareDistributions(
      { brokers: [load(102, 10), load(101, 30)], replicaSpread: 20, leaderSpread: 0, dataSpreadBytes: 0, score: 1 },
      { brokers: [load(101, 20), load(102, 20), load(103, 0, true)], replicaSpread: 0, leaderSpread: 0, dataSpreadBytes: 0, score: 0 },
    );
    expect(rows.map((r) => [r.id, r.before?.replicas, r.after?.replicas, r.excluded])).toEqual([
      [101, 30, 20, false],
      [102, 10, 20, false],
      [103, undefined, 0, true],
    ]);
  });

  it('summarizes partition moves', () => {
    expect(
      summarizeMoves([
        { replicaMoves: 1, leaderChange: true, sizeBytes: 100 },
        { replicaMoves: 2, leaderChange: false, sizeBytes: 50 },
        { replicaMoves: 0, leaderChange: true, sizeBytes: 70 },
      ]),
    ).toEqual({ partitions: 3, replicaMoves: 3, leaderChanges: 2, bytes: 200 });
  });

  it('computes disk usage', () => {
    const b = { diskTotalBytes: 200, diskUsableBytes: 50 } as Broker;
    expect(diskUsedPercent(b)).toBe(75);
    expect(diskUsedPercent({ diskTotalBytes: 0, diskUsableBytes: 0 } as Broker)).toBeNull();
  });
});
