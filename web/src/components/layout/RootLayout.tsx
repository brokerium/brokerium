// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { ModalsProvider } from '@mantine/modals';
import { Outlet } from 'react-router';

/** Root route; the modals provider lives inside the router so dialogs can use links and queries. */
export function RootLayout() {
  return (
    <ModalsProvider>
      <Outlet />
    </ModalsProvider>
  );
}
