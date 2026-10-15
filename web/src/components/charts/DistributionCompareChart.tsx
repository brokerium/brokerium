// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import '@mantine/charts/styles.css';

import { BarChart } from '@mantine/charts';

import type { BrokerComparisonRow } from '../../lib/distribution';
import { formatBytes, formatNumber } from '../../lib/format';

export type CompareMetric = 'replicas' | 'leaders' | 'dataBytes';

interface DistributionCompareChartProps {
  rows: BrokerComparisonRow[];
  metric: CompareMetric;
  height?: number;
}

/** Before vs after bars per broker for a plan preview. */
export default function DistributionCompareChart({ rows, metric, height = 240 }: DistributionCompareChartProps) {
  const data = rows.map((r) => ({
    broker: String(r.id),
    Before: r.before?.[metric] ?? 0,
    After: r.after?.[metric] ?? 0,
  }));
  return (
    <BarChart
      h={height}
      data={data}
      dataKey="broker"
      withLegend
      tickLine="y"
      xAxisLabel="Broker"
      valueFormatter={metric === 'dataBytes' ? (v) => formatBytes(v) : (v) => formatNumber(v)}
      series={[
        { name: 'Before', color: 'gray.5' },
        { name: 'After', color: 'indigo.6' },
      ]}
    />
  );
}
