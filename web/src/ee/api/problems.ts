// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { api } from '../../api/client';
import { clusterPath } from '../../api/clusters';
import type { OperationParams, Severity } from '../../api/types';

export type ProblemObjectKind = 'cluster' | 'broker' | 'topic' | 'node';

/** What a problem is about. */
export interface ProblemObject {
  kind: ProblemObjectKind;
  /** The cluster id, the broker id, the topic name or the id of the agent node. */
  id: string;
  /** The name of a cluster or an agent node. */
  name?: string;
}

export interface Evidence {
  key: string;
  value: string;
}

export interface ChangeReplicationFactorParams {
  topics: string[];
  replicationFactor: number;
  throttleBytesPerSec?: number;
  batchSize?: number;
  rackAware?: boolean;
}

export interface AlterTopicConfigsParams {
  topics: string[];
  set?: Record<string, string>;
  delete?: string[];
}

export interface AlignBrokerConfigsParams {
  keys: string[];
  /** The cluster-wide value of keys; the value most brokers have by default. */
  values?: Record<string, string>;
}

/** An operation that resolves a problem, with the parameters to plan it. */
export type ProblemFix =
  | { operation: 'change-replication-factor'; label: string; params: ChangeReplicationFactorParams }
  | { operation: 'alter-topic-configs'; label: string; params: AlterTopicConfigsParams }
  | { operation: 'align-broker-configs'; label: string; params: AlignBrokerConfigsParams }
  | { operation: 'rebalance'; label: string; params: OperationParams['rebalance'] }
  | { operation: 'finalize-upgrade'; label: string; params: OperationParams['finalize-upgrade'] };

/** A finding of the problem rules with its state; the first fix is the recommended one. */
export interface Problem {
  fingerprint: string;
  code: string;
  severity: Severity;
  object: ProblemObject;
  title: string;
  explanation: string;
  evidence: Evidence[];
  recommendation: string;
  fixes: ProblemFix[];
  firstSeen: string;
  lastSeen: string;
  acknowledgedBy?: string;
  acknowledgedAt?: string;
  /** Set while the problem is muted. */
  mutedUntil?: string;
}

/** GET /clusters/{id}/problems */
export interface ProblemReport {
  problems: Problem[];
  /** The number of muted problems; they are listed with ?muted=true only. */
  muted: number;
  evaluatedAt: string;
}

/** GET /problems: the problems of a cluster by severity; muted ones count only as muted. */
export interface ProblemCounts {
  clusterId: string;
  critical: number;
  warning: number;
  info: number;
  muted: number;
  evaluatedAt: string;
}

export const problemKeys = {
  all: ['problems'] as const,
  counts: ['problems', 'counts'] as const,
  cluster: (clusterId: string, muted: boolean) => ['problems', 'cluster', clusterId, muted] as const,
};

/** The problems of a cluster; enabled only while the license covers them. */
export function useProblems(clusterId: string, muted: boolean, enabled: boolean) {
  return useQuery({
    queryKey: problemKeys.cluster(clusterId, muted),
    queryFn: () => api.get<ProblemReport>(clusterPath(clusterId, `/problems${muted ? '?muted=true' : ''}`)),
    enabled,
    refetchInterval: 15_000,
  });
}

/** The counts of every cluster; enabled only while the license covers them. */
export function useProblemCounts(enabled: boolean) {
  return useQuery({
    queryKey: problemKeys.counts,
    queryFn: async () => (await api.get<{ clusters: ProblemCounts[] }>('/problems')).clusters,
    enabled,
    refetchInterval: 10_000,
    meta: { silent: true },
  });
}

export type ProblemAction =
  | { kind: 'acknowledge' }
  | { kind: 'unacknowledge' }
  | { kind: 'mute'; until: string }
  | { kind: 'unmute' };

export function useProblemAction(clusterId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ fingerprint, action }: { fingerprint: string; action: ProblemAction }) => {
      const path = clusterPath(clusterId, `/problems/${encodeURIComponent(fingerprint)}`);
      switch (action.kind) {
        case 'acknowledge':
          return api.post<unknown>(`${path}/acknowledge`);
        case 'unacknowledge':
          return api.del<unknown>(`${path}/acknowledge`);
        case 'mute':
          return api.post<unknown>(`${path}/mute`, { until: action.until });
        case 'unmute':
          return api.del<unknown>(`${path}/mute`);
        default: {
          const exhaustive: never = action;
          return exhaustive;
        }
      }
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: problemKeys.all }),
    meta: { errorTitle: 'Could not change the problem' },
  });
}
