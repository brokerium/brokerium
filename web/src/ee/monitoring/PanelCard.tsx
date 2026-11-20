// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

import '@mantine/charts/styles.css';

import { AreaChart, LineChart } from '@mantine/charts';
import { Center, Group, Loader, Paper, Text, Tooltip } from '@mantine/core';
import { useInViewport } from '@mantine/hooks';
import { IconInfoCircle } from '@tabler/icons-react';
import { type ReactNode, useMemo } from 'react';

import { errorMessage } from '../../api/errors';
import { formatDateTime } from '../../lib/format';
import { type MonitoringPanel, type MonitoringRange, usePanelData } from '../api/monitoring';
import { chartModel, formatValue, timeTick } from './chart';

const HEIGHT = 200;
/** Panels with more series show them in the tooltip only. */
const MAX_LEGEND = 12;

interface PanelCardProps {
  clusterId: string;
  panel: MonitoringPanel;
  range: MonitoringRange;
}

/** A chart of the Monitoring tab; it queries its data once it is scrolled into view. */
export function PanelCard({ clusterId, panel, range }: PanelCardProps) {
  const { ref, inViewport } = useInViewport();
  const query = usePanelData(clusterId, panel.id, range, inViewport);
  const data = query.data;
  const model = useMemo(() => (data ? chartModel(data) : null), [data]);

  let body;
  if (query.error) {
    body = (
      <Center h={HEIGHT} px="md">
        <Text size="sm" c="red" ta="center">
          {errorMessage(query.error)}
        </Text>
      </Center>
    );
  } else if (!data || !model) {
    body = <Center h={HEIGHT}>{query.isFetching && <Loader size="sm" />}</Center>;
  } else if (data.series.length === 0) {
    body = (
      <Center h={HEIGHT} px="md">
        <Text size="sm" c="dimmed" ta="center">
          No data for this selector and time range.
        </Text>
      </Center>
    );
  } else {
    const common = {
      h: HEIGHT,
      data: model.rows,
      dataKey: 'time',
      withDots: false,
      curveType: 'linear' as const,
      connectNulls: false,
      strokeWidth: 1.5,
      tooltipAnimationDuration: 0,
      valueFormatter: (value: number) => formatValue(panel.unit, value),
      xAxisProps: { tickFormatter: timeTick(range), minTickGap: 48 },
      yAxisProps: { width: 84 },
      tooltipProps: { labelFormatter: (label: ReactNode) => (typeof label === 'number' ? formatDateTime(label) : label) },
      withLegend: model.series.length > 1 && model.series.length <= MAX_LEGEND,
    };
    body =
      panel.section === 'cluster' ? (
        <AreaChart {...common} series={model.series} withGradient fillOpacity={0.15} />
      ) : (
        <LineChart {...common} series={model.series} />
      );
  }

  return (
    <Paper ref={ref} withBorder radius="md" p="sm">
      <Group justify="space-between" mb="xs" wrap="nowrap" gap="xs">
        <Text fw={600} size="sm" truncate>
          {panel.title}
        </Text>
        {panel.description && (
          <Tooltip label={panel.description} multiline maw={300} withArrow>
            <IconInfoCircle size={16} color="var(--mantine-color-dimmed)" style={{ flexShrink: 0 }} />
          </Tooltip>
        )}
      </Group>
      {body}
      {data?.truncated && (
        <Text size="xs" c="dimmed" mt={4}>
          Showing the first {data.series.length} series; narrow the selector to see the others.
        </Text>
      )}
    </Paper>
  );
}
