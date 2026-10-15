// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { MantineProvider } from '@mantine/core';
import { Notifications } from '@mantine/notifications';
import { QueryClientProvider } from '@tanstack/react-query';
import { createBrowserRouter } from 'react-router';
import { RouterProvider } from 'react-router/dom';

import { setUnauthorizedHandler } from './api/client';
import { createQueryClient } from './api/queryClient';
import { routes } from './routes';
import { theme } from './theme';

const queryClient = createQueryClient();
const router = createBrowserRouter(routes);

setUnauthorizedHandler(() => {
  const { pathname, search } = router.state.location;
  if (pathname === '/login') return;
  void router.navigate(`/login?next=${encodeURIComponent(pathname + search)}`, { replace: true });
});

export function App() {
  return (
    <MantineProvider theme={theme} defaultColorScheme="auto">
      <Notifications position="top-right" limit={5} />
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </MantineProvider>
  );
}
