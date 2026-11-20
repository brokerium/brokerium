// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

import { describe, expect, it } from 'vitest';

import type { Problem, ProblemFix } from '../api/problems';
import { countsSummary, fixReady, groupBySeverity, muteUntil, objectLink, severityTitle } from './problems';

function problem(code: string, severity: Problem['severity']): Problem {
  return {
    fingerprint: code,
    code,
    severity,
    object: { kind: 'cluster', id: 'c1' },
    title: code,
    explanation: '',
    evidence: [],
    recommendation: '',
    fixes: [],
    firstSeen: '2026-11-20T12:00:00Z',
    lastSeen: '2026-11-20T12:00:00Z',
  };
}

describe('problems', () => {
  it('groups by severity, the most severe first', () => {
    const groups = groupBySeverity([problem('a', 'info'), problem('b', 'critical'), problem('c', 'info')]);
    expect(groups.map((g) => [g.severity, g.problems.map((p) => p.code)])).toEqual([
      ['critical', ['b']],
      ['info', ['a', 'c']],
    ]);
    expect(groupBySeverity([])).toEqual([]);
    expect(severityTitle('warning')).toBe('Warnings');
  });

  it('links objects to their pages', () => {
    expect(objectLink('c 1', { kind: 'topic', id: 'orders.v1' })).toEqual({
      label: 'Topic orders.v1',
      to: '/clusters/c%201/topics/orders.v1',
    });
    expect(objectLink('c1', { kind: 'broker', id: '101' }).to).toBe('/clusters/c1/brokers');
    expect(objectLink('c1', { kind: 'node', id: 'n1', name: 'kafka-1' })).toEqual({ label: 'Node kafka-1', to: '/nodes' });
    expect(objectLink('c1', { kind: 'cluster', id: 'c1' }).to).toBeUndefined();
  });

  it('plans fixes with complete parameters only', () => {
    const rf: ProblemFix = { operation: 'change-replication-factor', label: '', params: { topics: ['t'], replicationFactor: 3 } };
    expect(fixReady(rf)).toBe(true);
    expect(fixReady({ ...rf, params: { topics: ['t'], replicationFactor: 0 } })).toBe(false);
    const configs: ProblemFix = {
      operation: 'alter-topic-configs',
      label: '',
      params: { topics: ['t'], set: { 'min.insync.replicas': ' ' } },
    };
    expect(fixReady(configs)).toBe(false);
    expect(fixReady({ operation: 'rebalance', label: '', params: { mode: 'even' } })).toBe(true);
  });

  it('mutes until a time and summarizes counts', () => {
    expect(muteUntil(3_600_000, Date.UTC(2026, 10, 20, 12))).toBe('2026-11-20T13:00:00.000Z');
    const counts = { clusterId: 'c1', critical: 1, warning: 2, info: 0, muted: 1, evaluatedAt: '' };
    expect(countsSummary(counts)).toBe('1 critical, 2 warnings, 0 info; 1 muted');
    expect(countsSummary({ ...counts, warning: 1, muted: 0 })).toBe('1 critical, 1 warning, 0 info');
  });
});
