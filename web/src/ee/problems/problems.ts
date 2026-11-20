// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

import { clusterPath } from '../../api/clusters';
import type { Severity } from '../../api/types';
import { pluralize } from '../../lib/format';
import type { Problem, ProblemCounts, ProblemFix, ProblemObject } from '../api/problems';

const SEVERITIES: Severity[] = ['critical', 'warning', 'info'];

export interface SeverityGroup {
  severity: Severity;
  problems: Problem[];
}

/** Groups problems by severity, the most severe first, without empty groups. */
export function groupBySeverity(problems: Problem[]): SeverityGroup[] {
  return SEVERITIES.map((severity) => ({ severity, problems: problems.filter((p) => p.severity === severity) })).filter(
    (g) => g.problems.length > 0,
  );
}

export function severityTitle(severity: Severity): string {
  switch (severity) {
    case 'critical':
      return 'Critical';
    case 'warning':
      return 'Warnings';
    case 'info':
      return 'Information';
    default: {
      const exhaustive: never = severity;
      return exhaustive;
    }
  }
}

/** The name of the object of a problem and the page that shows it, if any. */
export function objectLink(clusterId: string, object: ProblemObject): { label: string; to?: string } {
  switch (object.kind) {
    case 'topic':
      return { label: `Topic ${object.id}`, to: clusterPath(clusterId, `/topics/${encodeURIComponent(object.id)}`) };
    case 'broker':
      return { label: `Broker ${object.id}`, to: clusterPath(clusterId, '/brokers') };
    case 'node':
      return { label: `Node ${object.name ?? object.id}`, to: '/nodes' };
    case 'cluster':
      return { label: 'Cluster' };
    default: {
      const exhaustive: never = object.kind;
      return exhaustive;
    }
  }
}

export const MUTE_PERIODS: { label: string; ms: number }[] = [
  { label: '1 hour', ms: 60 * 60_000 },
  { label: '1 day', ms: 24 * 60 * 60_000 },
  { label: '7 days', ms: 7 * 24 * 60 * 60_000 },
  { label: '30 days', ms: 30 * 24 * 60 * 60_000 },
];

/** The end of a mute that starts now, as the API expects it. */
export function muteUntil(ms: number, now: number = Date.now()): string {
  return new Date(now + ms).toISOString();
}

/** Whether the edited parameters of a fix can be planned. */
export function fixReady(fix: ProblemFix): boolean {
  switch (fix.operation) {
    case 'change-replication-factor':
      return Number.isInteger(fix.params.replicationFactor) && fix.params.replicationFactor >= 1;
    case 'alter-topic-configs':
      return Object.values(fix.params.set ?? {}).every((v) => v.trim() !== '');
    case 'align-broker-configs':
    case 'rebalance':
    case 'finalize-upgrade':
      return true;
    default: {
      const exhaustive: never = fix;
      return exhaustive;
    }
  }
}

/** "1 critical, 2 warnings, 1 info; 3 muted" */
export function countsSummary(c: ProblemCounts): string {
  const parts = [`${c.critical} critical`, pluralize(c.warning, 'warning'), `${c.info} info`];
  const text = parts.join(', ');
  return c.muted > 0 ? `${text}; ${c.muted} muted` : text;
}
