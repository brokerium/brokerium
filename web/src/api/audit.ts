// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { useQuery } from '@tanstack/react-query';

import { queryString } from '../lib/format';
import { api } from './client';
import type { AuditEntry } from './types';

export interface AuditResponse {
  entries: AuditEntry[];
}

export const auditKeys = {
  list: (clusterId: string | undefined, limit: number) => ['audit', clusterId ?? 'all', limit] as const,
};

export function useAudit(clusterId: string | undefined, limit: number) {
  return useQuery({
    queryKey: auditKeys.list(clusterId, limit),
    queryFn: async () => (await api.get<AuditResponse>(`/audit${queryString({ clusterId, limit })}`)).entries,
    refetchInterval: 15_000,
  });
}
