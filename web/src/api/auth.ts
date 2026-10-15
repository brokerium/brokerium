// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { api } from './client';
import type { User } from './types';

export interface LoginRequest {
  username: string;
  password: string;
}

export interface LoginResponse {
  user: User;
  token: string;
  expiresAt: string;
}

export interface MeResponse {
  user: User;
}

/** GET /version (public). */
export interface VersionInfo {
  version: string;
  commit: string;
  date: string;
}

export const authKeys = {
  me: ['auth', 'me'] as const,
  version: ['version'] as const,
};

export function useMe() {
  return useQuery({
    queryKey: authKeys.me,
    queryFn: async () => (await api.get<MeResponse>('/auth/me')).user,
    staleTime: 5 * 60_000,
    retry: false,
    meta: { silent: true },
  });
}

export function useLogin() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (req: LoginRequest) => api.post<LoginResponse>('/auth/login', req),
    onSuccess: (res) => {
      qc.removeQueries();
      qc.setQueryData(authKeys.me, res.user);
    },
    meta: { silent: true },
  });
}

export function useLogout() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.post<void>('/auth/logout'),
    onSettled: () => qc.clear(),
    meta: { silent: true },
  });
}

export function useVersion() {
  return useQuery({
    queryKey: authKeys.version,
    queryFn: () => api.get<VersionInfo>('/version'),
    staleTime: Infinity,
    meta: { silent: true },
  });
}
