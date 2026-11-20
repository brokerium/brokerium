// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { useQuery } from '@tanstack/react-query';

import { api } from './client';
import type { ServerInfo } from './types';

export const serverKeys = {
  info: ['server'] as const,
};

/** Version, edition and the cluster providers this server can create. */
export function useServerInfo() {
  return useQuery({
    queryKey: serverKeys.info,
    queryFn: () => api.get<ServerInfo>('/server'),
    staleTime: 60_000,
    meta: { silent: true },
  });
}

/** The Kafka release the server gives new clusters; undefined until the server answered. */
export function useDefaultKafkaVersion(): string | undefined {
  return useServerInfo().data?.defaultKafkaVersion;
}
