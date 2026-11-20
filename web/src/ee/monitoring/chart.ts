// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

import { formatBytes, formatBytesPerSecond, formatDuration, formatNumber, formatPercent, formatRate } from '../../lib/format';
import type { MonitoringRange, MonitoringSection, MonitoringUnit, PanelData } from '../api/monitoring';

export const RANGES: { value: MonitoringRange; label: string }[] = [
  { value: '1h', label: '1h' },
  { value: '6h', label: '6h' },
  { value: '24h', label: '24h' },
  { value: '7d', label: '7d' },
];

export const SECTIONS: { value: MonitoringSection; label: string }[] = [
  { value: 'cluster', label: 'Cluster' },
  { value: 'broker', label: 'Brokers' },
  { value: 'topic', label: 'Topics' },
];

const COLORS = ['blue.6', 'teal.6', 'orange.6', 'grape.6', 'red.6', 'cyan.6', 'lime.7', 'pink.6', 'indigo.6', 'yellow.7'];

function trimmed(value: number, digits: number): string {
  const text = value.toFixed(digits);
  return text.includes('.') ? text.replace(/\.?0+$/, '') : text;
}

function formatMilliseconds(value: number): string {
  if (value >= 1000) return formatDuration(value);
  return `${trimmed(value, value < 10 ? 1 : 0)} ms`;
}

/** Formats a value of a panel by its unit, for the axis and the tooltip. */
export function formatValue(unit: MonitoringUnit, value: number): string {
  switch (unit) {
    case 'bytes':
      return formatBytes(value);
    case 'bytesPerSecond':
      return formatBytesPerSecond(value);
    case 'messagesPerSecond':
      return formatRate(value, ' msg/s');
    case 'requestsPerSecond':
      return formatRate(value, ' req/s');
    case 'perMinute':
      return formatRate(value, '/min');
    case 'milliseconds':
      return formatMilliseconds(value);
    case 'percent':
      return formatPercent(value);
    case 'cores':
      return `${trimmed(value, 2)} cores`;
    case 'count':
      return formatNumber(Number(trimmed(value, 2)));
    default: {
      const exhaustive: never = unit;
      return exhaustive;
    }
  }
}

export interface ChartSeriesModel {
  /** Key in the rows; series names can contain dots, which chart libraries read as paths. */
  name: string;
  label: string;
  color: string;
}

export interface ChartModel {
  /** One row per step: time in Unix milliseconds and a value per series key. */
  rows: Record<string, number>[];
  series: ChartSeriesModel[];
}

/** Turns panel data into chart rows with one row for every step of the range, so gaps stay visible. */
export function chartModel(data: PanelData): ChartModel {
  const step = Math.max(1, data.step);
  const rows: Record<string, number>[] = [];
  const index = new Map<number, Record<string, number>>();
  for (let t = data.start; t <= data.end && rows.length <= 2000; t += step) {
    const row = { time: t * 1000 };
    rows.push(row);
    index.set(t, row);
  }
  const series = data.series.map((s, i) => {
    const name = `s${i}`;
    for (const [t, value] of s.points) {
      const at = Math.round(t);
      let row = index.get(at);
      if (!row) {
        row = { time: at * 1000 };
        index.set(at, row);
        rows.push(row);
      }
      row[name] = value;
    }
    return { name, label: s.name, color: COLORS[i % COLORS.length] };
  });
  rows.sort((a, b) => a.time - b.time);
  return { rows, series };
}

const timeOfDay = new Intl.DateTimeFormat(undefined, { hour: '2-digit', minute: '2-digit' });
const dayAndTime = new Intl.DateTimeFormat(undefined, { weekday: 'short', hour: '2-digit', minute: '2-digit' });

/** Formats the ticks of the time axis: the time of day, with the weekday for ranges longer than a day. */
export function timeTick(range: MonitoringRange): (ms: number) => string {
  return (ms) => (range === '7d' ? dayAndTime : timeOfDay).format(ms);
}
