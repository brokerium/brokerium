// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

import { Alert, Anchor, Badge, Button, Code, EmptyState, Group, Menu, Paper, Stack, Switch, Text, Title } from '@mantine/core';
import { IconBell, IconBellOff, IconBulb, IconCheck, IconChevronDown, IconCircleCheck, IconTool } from '@tabler/icons-react';
import { useState } from 'react';
import { Link } from 'react-router';

import { EnterpriseBadge } from '../../components/badges';
import { GatedButton, GatedMenuItem } from '../../components/Gated';
import { usePlanner } from '../../components/operations/plannerContext';
import { PageLoader, QueryError } from '../../components/QueryState';
import { RelativeTime } from '../../components/RelativeTime';
import { useFeature } from '../../hooks/useFeature';
import { usePermissions } from '../../hooks/usePermissions';
import { withRole } from '../../lib/capabilities';
import { formatDateTime, pluralize } from '../../lib/format';
import { severityColor } from '../../lib/status';
import { useClusterContext } from '../../routes/cluster/clusterContext';
import { type Problem, type ProblemAction, type ProblemFix, useProblemAction, useProblems } from '../api/problems';
import { PROBLEMS } from '../features';
import { FixModal } from './FixModal';
import { groupBySeverity, MUTE_PERIODS, muteUntil, objectLink, severityTitle } from './problems';

interface Fixing {
  problem: Problem;
  fix: ProblemFix;
}

export default function ProblemsTab() {
  const { clusterId } = useClusterContext();
  const { availability } = useFeature(PROBLEMS);
  const [showMuted, setShowMuted] = useState(false);
  const [fixing, setFixing] = useState<Fixing | null>(null);
  const report = useProblems(clusterId, showMuted, availability.allowed);

  if (!availability.allowed) {
    return (
      <Alert
        title={
          <Group gap="xs">
            Problems <EnterpriseBadge size="xs" />
          </Group>
        }
      >
        <Text size="sm">
          {availability.reason}. Problems explain risks in the settings and the placement of a cluster and plan their
          fixes. See <Link to="/license">License</Link> for the features of Brokerium Enterprise.
        </Text>
      </Alert>
    );
  }
  if (report.isPending) return <PageLoader label="Checking the cluster" />;
  if (!report.data) {
    return <QueryError error={report.error} title="Could not load the problems" onRetry={() => void report.refetch()} />;
  }

  const { problems, muted, evaluatedAt } = report.data;
  const groups = groupBySeverity(problems);
  return (
    <Stack gap="md">
      <Group justify="space-between">
        <Text size="sm" c="dimmed">
          {pluralize(problems.length, 'problem')} · checked <RelativeTime value={evaluatedAt} size="sm" c="dimmed" />
        </Text>
        {(muted > 0 || showMuted) && (
          <Switch
            label={`Show muted (${muted})`}
            checked={showMuted}
            onChange={(e) => setShowMuted(e.currentTarget.checked)}
          />
        )}
      </Group>
      {groups.length === 0 && (
        <Paper withBorder radius="md" p="xl">
          <EmptyState
            icon={<IconCircleCheck size={28} />}
            title="No problems found"
            description="The rules found no risk in the settings, the replication and the placement of this cluster."
          />
        </Paper>
      )}
      {groups.map((group) => (
        <Stack key={group.severity} gap="xs">
          <Group gap="xs">
            <Title order={5}>{severityTitle(group.severity)}</Title>
            <Badge color={severityColor(group.severity)} variant="light" size="sm" circle={group.problems.length < 10}>
              {group.problems.length}
            </Badge>
          </Group>
          {group.problems.map((p) => (
            <ProblemCard key={p.fingerprint} problem={p} onFix={(fix) => setFixing({ problem: p, fix })} />
          ))}
        </Stack>
      ))}
      {fixing && <FixModal problem={fixing.problem} fix={fixing.fix} onClose={() => setFixing(null)} />}
    </Stack>
  );
}

interface ProblemCardProps {
  problem: Problem;
  onFix: (fix: ProblemFix) => void;
}

function ProblemCard({ problem: p, onFix }: ProblemCardProps) {
  const { clusterId } = useClusterContext();
  const planner = usePlanner();
  const { user } = usePermissions();
  const action = useProblemAction(clusterId);
  const operate = withRole({ allowed: true }, user?.role);
  const object = objectLink(clusterId, p.object);
  const [recommended, ...alternatives] = p.fixes;
  const run = (a: ProblemAction) => action.mutate({ fingerprint: p.fingerprint, action: a });

  return (
    <Paper withBorder radius="md" p="md" style={p.mutedUntil ? { opacity: 0.75 } : undefined}>
      <Stack gap="xs">
        <Group gap="xs">
          <Badge color={severityColor(p.severity)} variant={p.severity === 'critical' ? 'filled' : 'light'} size="sm">
            {p.severity}
          </Badge>
          <Text fw={600}>{p.title}</Text>
          {p.acknowledgedBy && (
            <Badge color="gray" variant="outline" size="sm" tt="none" leftSection={<IconCheck size={12} />}>
              acknowledged by {p.acknowledgedBy}
            </Badge>
          )}
          {p.mutedUntil && (
            <Badge color="gray" variant="outline" size="sm" tt="none" leftSection={<IconBellOff size={12} />}>
              muted until {formatDateTime(p.mutedUntil)}
            </Badge>
          )}
        </Group>
        <Group gap={6}>
          {object.to ? (
            <Anchor component={Link} to={object.to} size="sm">
              {object.label}
            </Anchor>
          ) : (
            <Text size="sm">{object.label}</Text>
          )}
          <Text size="sm" c="dimmed">
            ·
          </Text>
          <Code>{p.code}</Code>
          <Text size="sm" c="dimmed">
            · first seen
          </Text>
          <RelativeTime value={p.firstSeen} size="sm" c="dimmed" />
        </Group>
        <Text size="sm">{p.explanation}</Text>
        {p.evidence.length > 0 && (
          <Group gap="lg">
            {p.evidence.map((e) => (
              <Text key={e.key} size="sm">
                <Text span c="dimmed">
                  {e.key}:
                </Text>{' '}
                <Text span ff="monospace">
                  {e.value}
                </Text>
              </Text>
            ))}
          </Group>
        )}
        <Group gap={6} align="flex-start" wrap="nowrap">
          <IconBulb size={16} style={{ flexShrink: 0, marginTop: 2 }} />
          <Text size="sm">{p.recommendation}</Text>
        </Group>
        <Group justify="space-between" mt={4}>
          <Group gap="xs">
            {recommended && (
              <GatedButton
                size="xs"
                availability={planner.namedAvailability(recommended.operation)}
                leftSection={<IconTool size={14} />}
                onClick={() => onFix(recommended)}
              >
                {recommended.label}
              </GatedButton>
            )}
            {alternatives.length > 0 && (
              <Menu position="bottom-start" withArrow>
                <Menu.Target>
                  <Button size="xs" variant="default" rightSection={<IconChevronDown size={12} />}>
                    Other fixes
                  </Button>
                </Menu.Target>
                <Menu.Dropdown>
                  {alternatives.map((fix) => (
                    <GatedMenuItem
                      key={fix.label}
                      availability={planner.namedAvailability(fix.operation)}
                      onClick={() => onFix(fix)}
                    >
                      {fix.label}
                    </GatedMenuItem>
                  ))}
                </Menu.Dropdown>
              </Menu>
            )}
          </Group>
          <Group gap="xs">
            <GatedButton
              size="xs"
              variant="default"
              availability={operate}
              leftSection={<IconCheck size={14} />}
              loading={action.isPending}
              onClick={() => run({ kind: p.acknowledgedBy ? 'unacknowledge' : 'acknowledge' })}
            >
              {p.acknowledgedBy ? 'Unacknowledge' : 'Acknowledge'}
            </GatedButton>
            {p.mutedUntil || !operate.allowed ? (
              <GatedButton
                size="xs"
                variant="default"
                availability={operate}
                leftSection={p.mutedUntil ? <IconBell size={14} /> : <IconBellOff size={14} />}
                loading={action.isPending}
                onClick={() => run({ kind: 'unmute' })}
              >
                {p.mutedUntil ? 'Unmute' : 'Mute'}
              </GatedButton>
            ) : (
              <Menu position="bottom-end" withArrow>
                <Menu.Target>
                  <Button
                    size="xs"
                    variant="default"
                    leftSection={<IconBellOff size={14} />}
                    rightSection={<IconChevronDown size={12} />}
                    loading={action.isPending}
                  >
                    Mute
                  </Button>
                </Menu.Target>
                <Menu.Dropdown>
                  {MUTE_PERIODS.map((m) => (
                    <Menu.Item key={m.label} onClick={() => run({ kind: 'mute', until: muteUntil(m.ms) })}>
                      For {m.label}
                    </Menu.Item>
                  ))}
                </Menu.Dropdown>
              </Menu>
            )}
          </Group>
        </Group>
      </Stack>
    </Paper>
  );
}
