// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { describe, expect, it } from 'vitest';

import type { Broker, Issue } from '../api/types';
import { brokerState, bySeverity, diskUsageColor, healthColor, isOperationActive, operationStatusColor } from './status';

describe('health colors', () => {
  it('maps health states to the dashboard palette', () => {
    expect(healthColor('ok')).toBe('green');
    expect(healthColor('warning')).toBe('yellow');
    expect(healthColor('critical')).toBe('red');
    expect(healthColor('unreachable')).toBe('gray');
  });

  it('sorts issues by severity', () => {
    const issues: Issue[] = [
      { severity: 'info', code: 'a', message: '' },
      { severity: 'critical', code: 'b', message: '' },
      { severity: 'warning', code: 'c', message: '' },
    ];
    expect([...issues].sort(bySeverity).map((i) => i.code)).toEqual(['b', 'c', 'a']);
  });
});

describe('operations', () => {
  it('polls only operations that can still change', () => {
    expect(isOperationActive('running')).toBe(true);
    expect(isOperationActive('paused')).toBe(true);
    expect(isOperationActive('planned')).toBe(false);
    expect(isOperationActive('succeeded')).toBe(false);
    expect(operationStatusColor('failed')).toBe('red');
  });
});

describe('brokers', () => {
  it('derives the broker state', () => {
    expect(brokerState({ alive: true, fenced: false } as Broker)).toBe('alive');
    expect(brokerState({ alive: false, fenced: true } as Broker)).toBe('fenced');
    expect(brokerState({ alive: false, fenced: false } as Broker)).toBe('offline');
  });

  it('colors disk usage by threshold', () => {
    expect(diskUsageColor(50)).toBe('indigo');
    expect(diskUsageColor(80)).toBe('yellow');
    expect(diskUsageColor(95)).toBe('red');
  });
});
