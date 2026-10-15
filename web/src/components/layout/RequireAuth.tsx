// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { Center, Container } from '@mantine/core';
import { Navigate, useLocation } from 'react-router';

import { useMe } from '../../api/auth';
import { isUnauthorized } from '../../api/errors';
import { FullPageLoader, QueryError } from '../QueryState';
import { AppLayout } from './AppLayout';

/** Layout route: loads the current user, redirects to /login when there is no session. */
export function RequireAuth() {
  const me = useMe();
  const location = useLocation();
  if (me.isPending) return <FullPageLoader />;
  if (me.isError) {
    if (isUnauthorized(me.error)) {
      const next = encodeURIComponent(location.pathname + location.search);
      return <Navigate to={`/login?next=${next}`} replace />;
    }
    return (
      <Center h="100vh">
        <Container size="sm">
          <QueryError error={me.error} title="Cannot load the session" onRetry={() => void me.refetch()} />
        </Container>
      </Center>
    );
  }
  return <AppLayout user={me.data} />;
}
