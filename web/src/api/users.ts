// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { api } from './client';
import type { Role, User } from './types';

export interface UsersResponse {
  users: User[];
}

export interface UserResponse {
  user: User;
}

export interface CreateUserRequest {
  username: string;
  password: string;
  role: Role;
}

export interface UpdateUserRequest {
  role?: Role;
  disabled?: boolean;
  password?: string;
}

const enc = encodeURIComponent;

export const userKeys = {
  all: ['users'] as const,
};

export function useUsers(enabled = true) {
  return useQuery({
    queryKey: userKeys.all,
    queryFn: async () => (await api.get<UsersResponse>('/users')).users,
    enabled,
  });
}

export function useCreateUser() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (req: CreateUserRequest) => api.post<UserResponse>('/users', req),
    onSuccess: () => qc.invalidateQueries({ queryKey: userKeys.all }),
    meta: { errorTitle: 'Could not create the user' },
  });
}

export function useUpdateUser() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, ...req }: UpdateUserRequest & { id: string }) => api.patch<UserResponse>(`/users/${enc(id)}`, req),
    onSuccess: () => qc.invalidateQueries({ queryKey: userKeys.all }),
    meta: { errorTitle: 'Could not update the user' },
  });
}

export function useDeleteUser() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.del<void>(`/users/${enc(id)}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: userKeys.all }),
    meta: { errorTitle: 'Could not delete the user' },
  });
}
