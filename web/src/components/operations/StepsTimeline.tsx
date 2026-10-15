// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { Badge, Button, Collapse, Group, Loader, Stack, Text, Timeline } from '@mantine/core';
import {
  IconChevronDown,
  IconChevronRight,
  IconCircleCheck,
  IconCircleDashed,
  IconCircleX,
  IconPlayerSkipForward,
} from '@tabler/icons-react';
import { useState } from 'react';

import type { Step, StepStatus } from '../../api/types';
import { hasContent } from '../../lib/messages';
import { stepStatusColor } from '../../lib/status';
import { JsonBlock } from '../JsonBlock';
import { LiveDuration } from '../LiveDuration';
import { RelativeTime } from '../RelativeTime';

function StepIcon({ status }: { status: StepStatus }) {
  switch (status) {
    case 'pending':
      return <IconCircleDashed size={14} />;
    case 'running':
      return <Loader size={12} color="white" />;
    case 'succeeded':
      return <IconCircleCheck size={14} />;
    case 'failed':
      return <IconCircleX size={14} />;
    case 'skipped':
      return <IconPlayerSkipForward size={14} />;
    default: {
      const exhaustive: never = status;
      return exhaustive;
    }
  }
}

function StepItemBody({ step }: { step: Step }) {
  const [open, setOpen] = useState(false);
  const withArgs = hasContent(step.args);
  const withDetail = hasContent(step.detail);
  return (
    <Stack gap={4}>
      <Group gap="xs">
        <Badge size="xs" variant="outline" color="gray">
          {step.kind}
        </Badge>
        <Badge size="xs" variant="light" color={stepStatusColor(step.status)}>
          {step.status}
        </Badge>
        {step.alwaysRun && (
          <Badge size="xs" variant="light" color="grape" title="Runs even after a failure or cancellation (cleanup)">
            always runs
          </Badge>
        )}
        {step.startedAt && (
          <Text size="xs" c="dimmed">
            <LiveDuration start={step.startedAt} end={step.finishedAt} />
          </Text>
        )}
        {step.startedAt && (
          <Text size="xs" c="dimmed">
            started <RelativeTime value={step.startedAt} size="xs" c="dimmed" />
          </Text>
        )}
      </Group>
      {step.error && (
        <Text size="sm" c="red" style={{ whiteSpace: 'pre-wrap' }}>
          {step.error}
        </Text>
      )}
      {(withArgs || withDetail) && (
        <>
          <Button
            variant="subtle"
            size="compact-xs"
            w="fit-content"
            leftSection={open ? <IconChevronDown size={12} /> : <IconChevronRight size={12} />}
            onClick={() => setOpen((o) => !o)}
          >
            {open ? 'Hide details' : 'Show details'}
          </Button>
          <Collapse expanded={open}>
            <Stack gap="xs">
              {withArgs && (
                <div>
                  <Text size="xs" c="dimmed" mb={2}>
                    Arguments
                  </Text>
                  <JsonBlock value={step.args} maxHeight={240} />
                </div>
              )}
              {withDetail && (
                <div>
                  <Text size="xs" c="dimmed" mb={2}>
                    Detail
                  </Text>
                  <JsonBlock value={step.detail} maxHeight={320} />
                </div>
              )}
            </Stack>
          </Collapse>
        </>
      )}
    </Stack>
  );
}

/** Steps of an operation as a vertical timeline. */
export function StepsTimeline({ steps }: { steps: Step[] }) {
  const ordered = [...steps].sort((a, b) => a.index - b.index);
  const active = ordered.reduce((last, s, i) => (s.status === 'pending' ? last : i), -1);
  return (
    <Timeline active={active} bulletSize={22} lineWidth={2}>
      {ordered.map((step) => (
        <Timeline.Item
          key={step.index}
          bullet={<StepIcon status={step.status} />}
          color={stepStatusColor(step.status)}
          title={
            <Text fw={600} size="sm" c={step.status === 'skipped' ? 'dimmed' : undefined}>
              {step.index + 1}. {step.name}
            </Text>
          }
          lineVariant={step.status === 'pending' ? 'dashed' : 'solid'}
        >
          <StepItemBody step={step} />
        </Timeline.Item>
      ))}
    </Timeline>
  );
}
