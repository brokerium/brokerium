// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

import { describe, expect, it } from 'vitest';

import type { MonitoringUnit, PanelData } from '../api/monitoring';
import { chartModel, formatValue } from './chart';

describe('formatValue', () => {
  it.each<[MonitoringUnit, number, string]>([
    ['bytes', 1536, '1.5 KiB'],
    ['bytesPerSecond', 3 * 1024 * 1024, '3 MiB/s'],
    ['messagesPerSecond', 1234.4, '1,234 msg/s'],
    ['requestsPerSecond', 2.25, '2.3 req/s'],
    ['perMinute', 0.5, '0.5/min'],
    ['milliseconds', 3.14159, '3.1 ms'],
    ['milliseconds', 250.4, '250 ms'],
    ['milliseconds', 1500, '1.5 s'],
    ['percent', 97.26, '97%'],
    ['cores', 1.5, '1.5 cores'],
    ['count', 3, '3'],
    ['count', 2.333, '2.33'],
  ])('%s %d', (unit, value, want) => {
    expect(formatValue(unit, value)).toBe(want);
  });
});

describe('chartModel', () => {
  const data: PanelData = {
    panel: 'broker-heap',
    unit: 'bytes',
    start: 100,
    end: 160,
    step: 15,
    truncated: false,
    series: [
      { name: 'broker 101', labels: { instance: 'kafka-1.example.com:9141' }, points: [[100, 1], [115, 2], [160, 5]] },
      { name: 'broker.102', labels: { instance: 'kafka-2:9141' }, points: [[130, 3], [137, 4]] },
    ],
  };

  it('has a row for every step and keys without dots', () => {
    const model = chartModel(data);
    expect(model.series).toEqual([
      { name: 's0', label: 'broker 101', color: 'blue.6' },
      { name: 's1', label: 'broker.102', color: 'teal.6' },
    ]);
    expect(model.rows).toEqual([
      { time: 100_000, s0: 1 },
      { time: 115_000, s0: 2 },
      { time: 130_000, s1: 3 },
      { time: 137_000, s1: 4 },
      { time: 145_000 },
      { time: 160_000, s0: 5 },
    ]);
  });

  it('handles data without series', () => {
    expect(chartModel({ ...data, series: [] }).rows).toHaveLength(5);
  });
});
