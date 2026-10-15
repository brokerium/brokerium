// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { queryString } from '../lib/format';
import { api } from './client';
import type { AgentNode, JoinTokenResponse } from './types';

export interface NodesResponse {
  nodes: AgentNode[];
}

export interface JoinTokenRequest {
  nodeName: string;
  clusterId?: string;
  ttlMinutes?: number;
}

export const nodeKeys = {
  all: ['nodes'] as const,
  list: (clusterId?: string) => ['nodes', clusterId ?? 'all'] as const,
  pools: (clusterId: string) => ['node-pools', clusterId] as const,
};

/** A Strimzi KafkaNodePool. */
export interface NodePool {
  name: string;
  roles: string[];
  replicas: number;
  readyReplicas: number;
  nodeIds: number[] | null;
  settled: boolean;
}

export function useNodePools(clusterId: string, enabled: boolean) {
  return useQuery({
    queryKey: nodeKeys.pools(clusterId),
    queryFn: async () =>
      (await api.get<{ pools: NodePool[] }>(`/clusters/${encodeURIComponent(clusterId)}/node-pools`)).pools,
    enabled,
    refetchInterval: 10_000,
  });
}

export function useNodes(clusterId?: string) {
  return useQuery({
    queryKey: nodeKeys.list(clusterId),
    queryFn: async () => (await api.get<NodesResponse>(`/nodes${queryString({ clusterId })}`)).nodes,
    refetchInterval: 10_000,
  });
}

export function useCreateJoinToken() {
  return useMutation({
    mutationFn: (req: JoinTokenRequest) => api.post<JoinTokenResponse>('/join-tokens', req),
    meta: { errorTitle: 'Could not create a join token' },
  });
}

export function useDeleteNode() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.del<void>(`/nodes/${encodeURIComponent(id)}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: nodeKeys.all }),
    meta: { errorTitle: 'Could not remove the node' },
  });
}
