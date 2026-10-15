// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import type { ComponentType } from 'react';
import type { RouteObject } from 'react-router';

import { RequireAuth } from '../components/layout/RequireAuth';
import { RootLayout } from '../components/layout/RootLayout';
import { RouteError } from '../components/layout/RouteError';
import { FullPageLoader } from '../components/QueryState';

/** Lazily loaded route component: every page is its own chunk. */
function page(load: () => Promise<{ default: ComponentType }>) {
  return { Component: async () => (await load()).default };
}

export const routes: RouteObject[] = [
  {
    Component: RootLayout,
    HydrateFallback: FullPageLoader,
    ErrorBoundary: RouteError,
    children: [
      { path: 'login', lazy: page(() => import('./LoginPage')) },
      {
        Component: RequireAuth,
        children: [
          {
            ErrorBoundary: RouteError,
            children: [
              { index: true, lazy: page(() => import('./OverviewPage')) },
              {
                path: 'clusters/:clusterId',
                lazy: page(() => import('./cluster/ClusterLayout')),
                children: [
                  {
                    ErrorBoundary: RouteError,
                    children: [
                      { index: true, lazy: page(() => import('./cluster/OverviewTab')) },
                      { path: 'brokers', lazy: page(() => import('./cluster/BrokersTab')) },
                      { path: 'controllers', lazy: page(() => import('./cluster/ControllersTab')) },
                      { path: 'topics', lazy: page(() => import('./cluster/TopicsTab')) },
                      { path: 'topics/:topic', lazy: page(() => import('./cluster/TopicPage')) },
                      { path: 'distribution', lazy: page(() => import('./cluster/DistributionTab')) },
                      { path: 'groups', lazy: page(() => import('./cluster/GroupsTab')) },
                      { path: 'groups/:group', lazy: page(() => import('./cluster/GroupPage')) },
                      { path: 'security', lazy: page(() => import('./cluster/SecurityTab')) },
                      { path: 'operations', lazy: page(() => import('./cluster/ClusterOperationsTab')) },
                      { path: 'upgrade', lazy: page(() => import('./cluster/UpgradeTab')) },
                      { path: 'settings', lazy: page(() => import('./cluster/SettingsTab')) },
                    ],
                  },
                ],
              },
              { path: 'operations', lazy: page(() => import('./OperationsPage')) },
              { path: 'operations/:operationId', lazy: page(() => import('./OperationPage')) },
              { path: 'nodes', lazy: page(() => import('./NodesPage')) },
              { path: 'users', lazy: page(() => import('./UsersPage')) },
              { path: 'audit', lazy: page(() => import('./AuditPage')) },
              { path: '*', lazy: page(() => import('./NotFoundPage')) },
            ],
          },
        ],
      },
    ],
  },
];
