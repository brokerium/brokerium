// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { type QueryClient, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { api } from './client';
import type {
  ClusterRequest,
  ClusterSummary,
  ClusterView,
  ConfigEntry,
  GroupDetail,
  Rates,
  TestResult,
} from './types';

export interface OverviewResponse {
  clusters: ClusterSummary[];
}

/** Body of PATCH .../configs for topics and brokers. */
export interface ConfigPatch {
  set: Record<string, string>;
  delete: string[];
}

export interface BrokerConfigsResponse {
  broker: number;
  configs: ConfigEntry[];
}

export type ResetTarget = 'earliest' | 'latest' | 'timestamp' | 'offset';

export interface ResetOffsetsRequest {
  topic: string;
  partitions?: number[];
  to: ResetTarget;
  /** Epoch milliseconds, for to = 'timestamp'. */
  timestamp?: number;
  offset?: number;
  dryRun: boolean;
}

export interface OffsetChange {
  topic: string;
  partition: number;
  from: number;
  to: number;
}

export interface ResetOffsetsResponse {
  offsets: OffsetChange[];
}

const enc = encodeURIComponent;

export const clusterKeys = {
  overview: ['overview'] as const,
  detail: (id: string) => ['clusters', id] as const,
  topic: (id: string, topic: string) => ['clusters', id, 'topics', topic] as const,
  brokerConfigs: (id: string, broker: number) => ['clusters', id, 'brokers', broker, 'configs'] as const,
  group: (id: string, group: string) => ['clusters', id, 'groups', group] as const,
  rates: (id: string) => ['clusters', id, 'rates'] as const,
};

export function clusterPath(id: string, suffix = ''): string {
  return `/clusters/${enc(id)}${suffix}`;
}

export function useOverview() {
  return useQuery({
    queryKey: clusterKeys.overview,
    queryFn: async () => (await api.get<OverviewResponse>('/overview')).clusters,
    refetchInterval: 10_000,
  });
}

export function useCluster(id: string | undefined) {
  return useQuery({
    queryKey: clusterKeys.detail(id ?? ''),
    queryFn: () => api.get<ClusterView>(clusterPath(id ?? '')),
    enabled: Boolean(id),
    refetchInterval: 10_000,
  });
}

/** Forces a fresh snapshot (POST /refresh) and stores it in the cache. */
export async function refreshClusterView(qc: QueryClient, id: string): Promise<ClusterView | undefined> {
  try {
    const view = await api.post<ClusterView>(clusterPath(id, '/refresh'));
    qc.setQueryData(clusterKeys.detail(id), view);
    return view;
  } catch {
    await qc.invalidateQueries({ queryKey: clusterKeys.detail(id), exact: true });
    return undefined;
  }
}

export function useRefreshCluster(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.post<ClusterView>(clusterPath(id, '/refresh')),
    onSuccess: (view) => {
      qc.setQueryData(clusterKeys.detail(id), view);
      void qc.invalidateQueries({ queryKey: clusterKeys.overview });
    },
    meta: { errorTitle: 'Refresh failed' },
  });
}

export function useTestCluster() {
  return useMutation({
    mutationFn: (req: ClusterRequest) => api.post<TestResult>('/clusters/test', req),
    meta: { errorTitle: 'Connection test failed' },
  });
}

export function useCreateCluster() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (req: ClusterRequest) => api.post<ClusterView>('/clusters', req),
    onSuccess: (view) => {
      qc.setQueryData(clusterKeys.detail(view.cluster.id), view);
      void qc.invalidateQueries({ queryKey: clusterKeys.overview });
    },
    meta: { errorTitle: 'Could not add the cluster' },
  });
}

export function useUpdateCluster(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (req: ClusterRequest) => api.patch<ClusterView>(clusterPath(id), req),
    onSuccess: (view) => {
      qc.setQueryData(clusterKeys.detail(id), view);
      void qc.invalidateQueries({ queryKey: clusterKeys.overview });
    },
    meta: { errorTitle: 'Could not update the cluster' },
  });
}

export function useDeleteCluster() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.del<void>(clusterPath(id)),
    onSuccess: (_data, id) => {
      qc.removeQueries({ queryKey: clusterKeys.detail(id) });
      void qc.invalidateQueries({ queryKey: clusterKeys.overview });
    },
    meta: { errorTitle: 'Could not delete the cluster' },
  });
}

export function useBrokerConfigs(id: string, broker: number | null) {
  return useQuery({
    queryKey: clusterKeys.brokerConfigs(id, broker ?? -1),
    queryFn: () => api.get<BrokerConfigsResponse>(clusterPath(id, `/brokers/${broker}/configs`)),
    enabled: broker !== null,
    meta: { silent: true },
  });
}

export function useAlterBrokerConfigs(id: string, broker: number) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (patch: ConfigPatch) => api.patch<unknown>(clusterPath(id, `/brokers/${broker}/configs`), patch),
    onSuccess: () => qc.invalidateQueries({ queryKey: clusterKeys.brokerConfigs(id, broker) }),
    meta: { errorTitle: 'Could not change broker configs' },
  });
}

export function useGroupDetail(id: string, group: string) {
  return useQuery({
    queryKey: clusterKeys.group(id, group),
    queryFn: () => api.get<GroupDetail>(clusterPath(id, `/groups/${enc(group)}`)),
    refetchInterval: 10_000,
  });
}

export function useResetOffsets(id: string, group: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (req: ResetOffsetsRequest) =>
      api.post<ResetOffsetsResponse>(clusterPath(id, `/groups/${enc(group)}/reset-offsets`), req),
    onSuccess: (_res, req) => {
      if (!req.dryRun) void qc.invalidateQueries({ queryKey: clusterKeys.group(id, group) });
    },
    meta: { errorTitle: 'Offset reset failed' },
  });
}

export function useDeleteGroup(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (group: string) => api.del<void>(clusterPath(id, `/groups/${enc(group)}`)),
    onSuccess: (_data, group) => {
      qc.removeQueries({ queryKey: clusterKeys.group(id, group) });
      void refreshClusterView(qc, id);
    },
    meta: { errorTitle: 'Could not delete the group' },
  });
}

export function useRates(id: string, enabled = true) {
  return useQuery({
    queryKey: clusterKeys.rates(id),
    queryFn: () => api.get<Rates>(clusterPath(id, '/rates')),
    enabled,
    refetchInterval: 15_000,
    retry: false,
    meta: { silent: true },
  });
}
