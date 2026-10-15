// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { notifications } from '@mantine/notifications';
import { MutationCache, QueryCache, QueryClient } from '@tanstack/react-query';

import { ApiError } from './client';
import { errorMessage, isNetworkError, isUnauthorized } from './errors';

interface BrokeriumQueryMeta extends Record<string, unknown> {
  /** Do not notify about errors of this query; the page renders them itself. */
  silent?: boolean;
}

interface BrokeriumMutationMeta extends Record<string, unknown> {
  /** Title of the error notification. */
  errorTitle?: string;
  /** Do not notify about errors of this mutation; the form renders them itself. */
  silent?: boolean;
}

declare module '@tanstack/react-query' {
  interface Register {
    queryMeta: BrokeriumQueryMeta;
    mutationMeta: BrokeriumMutationMeta;
  }
}

function notifyError(id: string | undefined, title: string, error: unknown) {
  notifications.show({
    id: isNetworkError(error) ? 'network-error' : id,
    color: 'red',
    title,
    message: errorMessage(error),
    autoClose: 8000,
  });
}

export function createQueryClient(): QueryClient {
  return new QueryClient({
    queryCache: new QueryCache({
      onError: (error, query) => {
        if (query.meta?.silent || isUnauthorized(error)) return;
        // The first load failure is rendered inline by the page; notify about failed refreshes.
        if (query.state.data === undefined) return;
        notifyError(`query-${query.queryHash}`, 'Refresh failed', error);
      },
    }),
    mutationCache: new MutationCache({
      onError: (error, _variables, _result, mutation) => {
        if (mutation.meta?.silent || isUnauthorized(error)) return;
        notifyError(undefined, mutation.meta?.errorTitle ?? 'Request failed', error);
      },
    }),
    defaultOptions: {
      queries: {
        staleTime: 5_000,
        refetchOnWindowFocus: false,
        retry: (failureCount, error) => {
          if (error instanceof ApiError && error.status < 500) return false;
          return failureCount < 2;
        },
      },
      mutations: { retry: false },
    },
  });
}
