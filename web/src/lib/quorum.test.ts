// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { describe, expect, it } from 'vitest';

import type { Broker, QuorumReplica } from '../api/types';
import { canRemoveVoter, classifyObservers, faultTolerance, quorumHint, voterRemovalImpact } from './quorum';

function replica(nodeId: number): QuorumReplica {
  return {
    nodeId,
    directoryId: `dir-${nodeId}`,
    role: 'observer',
    logEndOffset: 100,
    lag: 0,
    lastFetchTimestamp: 0,
    lastCaughtUpTimestamp: 0,
  };
}

function broker(id: number): Broker {
  return {
    id,
    host: `broker-${id}`,
    port: 9092,
    rack: '',
    fenced: false,
    alive: true,
    cordoned: false,
    logDirs: null,
    diskTotalBytes: 0,
    diskUsableBytes: 0,
    dataBytes: 0,
    replicas: 0,
    leaders: 0,
    preferredLeaders: 0,
  };
}

describe('faultTolerance', () => {
  it.each([
    [0, 0],
    [1, 0],
    [2, 0],
    [3, 1],
    [4, 1],
    [5, 2],
    [7, 3],
  ])('%d voters tolerate %d failures', (voters, expected) => {
    expect(faultTolerance(voters)).toBe(expected);
  });

  it('explains that even voter counts add nothing', () => {
    expect(quorumHint(3)).toBe('3 voters tolerate 1 failure.');
    expect(quorumHint(4)).toContain('adds no fault tolerance over 3');
    expect(quorumHint(1)).toBe('1 voter tolerates 0 failures.');
  });
});

describe('voter removal', () => {
  it('never allows removing the last voter', () => {
    expect(canRemoveVoter(1)).toBe(false);
    expect(canRemoveVoter(0)).toBe(false);
    expect(canRemoveVoter(2)).toBe(true);
  });

  it('reports the tolerance impact', () => {
    expect(voterRemovalImpact(3)).toEqual({ after: 2, toleranceBefore: 1, toleranceAfter: 0, reducesTolerance: true });
    expect(voterRemovalImpact(4).reducesTolerance).toBe(false);
  });
});

describe('classifyObservers', () => {
  it('marks observers that are brokers and lists controllers first', () => {
    const result = classifyObservers([replica(102), replica(4), replica(101)], [broker(101), broker(102)]);
    expect(result.map((o) => [o.nodeId, o.kind])).toEqual([
      [4, 'controller'],
      [101, 'broker'],
      [102, 'broker'],
    ]);
  });
});
