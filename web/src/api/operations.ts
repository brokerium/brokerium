// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import type { OperationTypeName } from '../lib/capabilities';
import { queryString } from '../lib/format';
import { isOperationActive } from '../lib/status';
import { api } from './client';
import { clusterKeys, clusterPath } from './clusters';
import type { Operation, OperationParams, OperationType } from './types';

export interface OperationTypesResponse {
  types: OperationType[];
}

export interface OperationsResponse {
  operations: Operation[];
}

export interface CreateOperationRequest<T extends OperationTypeName = OperationTypeName> {
  type: T;
  params: OperationParams[T];
}

export interface ConfirmOperationRequest {
  /** Required (the cluster name) when the operation is irreversible. */
  confirmName?: string;
}

export type OperationControl = 'pause' | 'resume' | 'cancel';

const enc = encodeURIComponent;

export const operationKeys = {
  types: ['operation-types'] as const,
  all: ['operations'] as const,
  list: (limit: number) => ['operations', 'list', limit] as const,
  detail: (id: string) => ['operations', 'detail', id] as const,
  cluster: (clusterId: string) => ['operations', 'cluster', clusterId] as const,
};

export function useOperationTypes() {
  return useQuery({
    queryKey: operationKeys.types,
    queryFn: async () => (await api.get<OperationTypesResponse>('/operation-types')).types,
    staleTime: Infinity,
    retry: false,
    meta: { silent: true },
  });
}

function anyActive(ops: Operation[] | undefined): boolean {
  return Boolean(ops?.some((op) => isOperationActive(op.status)));
}

export function useOperations(limit = 100) {
  return useQuery({
    queryKey: operationKeys.list(limit),
    queryFn: async () => (await api.get<OperationsResponse>(`/operations${queryString({ limit })}`)).operations,
    refetchInterval: (query) => (anyActive(query.state.data) ? 2_000 : 10_000),
  });
}

export function useClusterOperations(clusterId: string) {
  return useQuery({
    queryKey: operationKeys.cluster(clusterId),
    queryFn: async () => (await api.get<OperationsResponse>(clusterPath(clusterId, '/operations'))).operations,
    refetchInterval: (query) => (anyActive(query.state.data) ? 2_000 : 10_000),
  });
}

export function useOperation(id: string) {
  return useQuery({
    queryKey: operationKeys.detail(id),
    queryFn: () => api.get<Operation>(`/operations/${enc(id)}`),
    refetchInterval: (query) => {
      const status = query.state.data?.status;
      return status && isOperationActive(status) ? 2_000 : false;
    },
  });
}

export function useCreateOperation(clusterId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (req: CreateOperationRequest) => api.post<Operation>(clusterPath(clusterId, '/operations'), req),
    onSuccess: (op) => {
      qc.setQueryData(operationKeys.detail(op.id), op);
      void qc.invalidateQueries({ queryKey: operationKeys.cluster(clusterId) });
    },
    meta: { errorTitle: 'Could not plan the operation' },
  });
}

function useOperationUpdate() {
  const qc = useQueryClient();
  return (op: Operation) => {
    qc.setQueryData(operationKeys.detail(op.id), op);
    void qc.invalidateQueries({ queryKey: operationKeys.all });
    void qc.invalidateQueries({ queryKey: clusterKeys.detail(op.clusterId), exact: true });
  };
}

export function useConfirmOperation() {
  const update = useOperationUpdate();
  return useMutation({
    mutationFn: ({ id, ...body }: ConfirmOperationRequest & { id: string }) =>
      api.post<Operation>(`/operations/${enc(id)}/confirm`, body),
    onSuccess: update,
    meta: { errorTitle: 'Could not start the operation' },
  });
}

export function useControlOperation() {
  const update = useOperationUpdate();
  return useMutation({
    mutationFn: ({ id, action }: { id: string; action: OperationControl }) =>
      api.post<Operation>(`/operations/${enc(id)}/${action}`),
    onSuccess: update,
    meta: { errorTitle: 'Operation control failed' },
  });
}

export function useDiscardOperation() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (op: Pick<Operation, 'id' | 'clusterId'>) => api.del<void>(`/operations/${enc(op.id)}`),
    onSuccess: (_data, op) => {
      qc.removeQueries({ queryKey: operationKeys.detail(op.id) });
      void qc.invalidateQueries({ queryKey: operationKeys.all });
    },
    meta: { errorTitle: 'Could not discard the plan' },
  });
}

/** Titles of operation types, falling back to the type name. */
export function operationTitle(type: string, types: OperationType[] | undefined): string {
  return types?.find((t) => t.type === type)?.title ?? type;
}
