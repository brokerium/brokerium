// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { queryString } from '../lib/format';
import { api } from './client';
import type { AgentNode, JoinTokenResponse } from './types';

export interface NodesResponse {
  nodes: AgentNode[];
}

/** Without nodeName the node is named after the host name it reports; nodeId enrolls a node again. */
export interface JoinTokenRequest {
  nodeName?: string;
  clusterId?: string;
  ttlMinutes?: number;
  nodeId?: string;
}

/** PATCH /nodes/{id}; an empty advertisedHost resets it to the reported host name. */
export interface NodeUpdate {
  name?: string;
  advertisedHost?: string;
  rack?: string;
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

/** Agent nodes; refetchInterval polls faster while waiting for a new host to connect. */
export function useNodes(clusterId?: string, refetchInterval = 10_000) {
  return useQuery({
    queryKey: nodeKeys.list(clusterId),
    queryFn: async () => (await api.get<NodesResponse>(`/nodes${queryString({ clusterId })}`)).nodes,
    refetchInterval,
  });
}

export function useUpdateNode() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, update }: { id: string; update: NodeUpdate }) =>
      api.patch<AgentNode>(`/nodes/${encodeURIComponent(id)}`, update),
    onSuccess: () => qc.invalidateQueries({ queryKey: nodeKeys.all }),
    meta: { errorTitle: 'Could not update the node' },
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
