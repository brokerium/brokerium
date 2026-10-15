// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { queryString } from '../lib/format';
import { api } from './client';
import { clusterPath } from './clusters';
import type { Acl, QuotaEntity, QuotaEntry, ScramUser } from './types';

export interface AclsResponse {
  acls: Acl[];
}

export type ScramMechanism = ScramUser['mechanisms'][number]['mechanism'];

export interface ScramUsersResponse {
  users: ScramUser[];
}

export interface UpsertScramUserRequest {
  user: string;
  mechanism: ScramMechanism;
  password: string;
  iterations?: number;
}

export interface QuotasResponse {
  quotas: QuotaEntry[];
}

export interface AlterQuotaRequest {
  entity: QuotaEntity[];
  set: Record<string, number>;
  remove: string[];
}

const enc = encodeURIComponent;

export const securityKeys = {
  acls: (id: string) => ['clusters', id, 'acls'] as const,
  scram: (id: string) => ['clusters', id, 'scram-users'] as const,
  quotas: (id: string) => ['clusters', id, 'quotas'] as const,
};

export function useAcls(id: string, enabled = true) {
  return useQuery({
    queryKey: securityKeys.acls(id),
    queryFn: async () => (await api.get<AclsResponse>(clusterPath(id, '/acls'))).acls,
    enabled,
  });
}

export function useCreateAcl(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (acl: Acl) => api.post<unknown>(clusterPath(id, '/acls'), acl),
    onSuccess: () => qc.invalidateQueries({ queryKey: securityKeys.acls(id) }),
    meta: { errorTitle: 'Could not create the ACL' },
  });
}

export function useDeleteAcl(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (acl: Acl) => api.del<void>(clusterPath(id, '/acls'), acl),
    onSuccess: () => qc.invalidateQueries({ queryKey: securityKeys.acls(id) }),
    meta: { errorTitle: 'Could not delete the ACL' },
  });
}

export function useScramUsers(id: string, enabled = true) {
  return useQuery({
    queryKey: securityKeys.scram(id),
    queryFn: async () => (await api.get<ScramUsersResponse>(clusterPath(id, '/scram-users'))).users,
    enabled,
  });
}

export function useUpsertScramUser(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (req: UpsertScramUserRequest) => api.post<unknown>(clusterPath(id, '/scram-users'), req),
    onSuccess: () => qc.invalidateQueries({ queryKey: securityKeys.scram(id) }),
    meta: { errorTitle: 'Could not save the SCRAM credential' },
  });
}

export function useDeleteScramUser(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ user, mechanism }: { user: string; mechanism: ScramMechanism }) =>
      api.del<void>(clusterPath(id, `/scram-users/${enc(user)}${queryString({ mechanism })}`)),
    onSuccess: () => qc.invalidateQueries({ queryKey: securityKeys.scram(id) }),
    meta: { errorTitle: 'Could not delete the SCRAM credential' },
  });
}

export function useQuotas(id: string, enabled = true) {
  return useQuery({
    queryKey: securityKeys.quotas(id),
    queryFn: async () => (await api.get<QuotasResponse>(clusterPath(id, '/quotas'))).quotas,
    enabled,
  });
}

export function useAlterQuota(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (req: AlterQuotaRequest) => api.put<unknown>(clusterPath(id, '/quotas'), req),
    onSuccess: () => qc.invalidateQueries({ queryKey: securityKeys.quotas(id) }),
    meta: { errorTitle: 'Could not change the quota' },
  });
}
