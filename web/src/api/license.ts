// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { api } from './client';
import type { LicenseView } from './types';

export const licenseKeys = {
  all: ['license'] as const,
};

export function useLicense() {
  return useQuery({
    queryKey: licenseKeys.all,
    queryFn: () => api.get<LicenseView>('/license'),
    staleTime: 5 * 60_000,
  });
}

/** Stores a license key; the form shows the reason when the server refuses it. */
export function useSetLicense() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (key: string) => api.put<LicenseView>('/license', { key }),
    onSuccess: (view) => qc.setQueryData(licenseKeys.all, view),
    meta: { silent: true },
  });
}

export function useDeleteLicense() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.del<void>('/license'),
    onSuccess: () => qc.invalidateQueries({ queryKey: licenseKeys.all }),
    meta: { errorTitle: 'Could not remove the license key' },
  });
}
