// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

import { Anchor, Badge, Group, Text, Tooltip } from '@mantine/core';
import { IconLock } from '@tabler/icons-react';
import { Link } from 'react-router';

import { clusterPath } from '../../api/clusters';
import { useFeature } from '../../hooks/useFeature';
import { useProblemCounts } from '../api/problems';
import { PROBLEMS } from '../features';
import { countsSummary } from './problems';

/** The problems of a cluster by severity in the cluster table of the overview page. */
export function ProblemCountsCell({ clusterId }: { clusterId: string }) {
  const { availability } = useFeature(PROBLEMS);
  const counts = useProblemCounts(availability.allowed);
  if (!availability.allowed) {
    return (
      <Tooltip label={availability.reason} withArrow multiline maw={280}>
        <IconLock size={14} color="var(--mantine-color-dimmed)" aria-label={availability.reason} />
      </Tooltip>
    );
  }
  const c = counts.data?.find((x) => x.clusterId === clusterId);
  if (!c) {
    return (
      <Text size="sm" c="dimmed">
        —
      </Text>
    );
  }
  const to = clusterPath(clusterId, '/problems');
  if (c.critical + c.warning + c.info === 0) {
    return (
      <Anchor component={Link} to={to} size="sm" c="dimmed">
        None{c.muted > 0 ? ` (${c.muted} muted)` : ''}
      </Anchor>
    );
  }
  return (
    <Tooltip label={countsSummary(c)} withArrow openDelay={300}>
      <Anchor component={Link} to={to} underline="never">
        <Group gap={4} wrap="nowrap">
          {c.critical > 0 && (
            <Badge color="red" size="sm">
              {c.critical}
            </Badge>
          )}
          {c.warning > 0 && (
            <Badge color="yellow" variant="light" size="sm">
              {c.warning}
            </Badge>
          )}
          {c.info > 0 && (
            <Badge color="blue" variant="light" size="sm">
              {c.info}
            </Badge>
          )}
        </Group>
      </Anchor>
    </Tooltip>
  );
}
