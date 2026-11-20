// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0
//
// The edition module of Community builds: the exports of src/ee without Enterprise features.

import type { RouteObject } from 'react-router';

import type {
  ClusterTabExtension,
  DashboardEdition,
  NavExtension,
  OverviewColumn,
  TopicAction,
  TopicTab,
} from '../lib/edition';

export const dashboardEdition: DashboardEdition = { edition: 'community' };

export const enterpriseRoutes: RouteObject[] = [];

export const navExtensions: NavExtension[] = [];

export const clusterTabs: ClusterTabExtension[] = [];

export const overviewColumns: OverviewColumn[] = [];

export const topicActions: TopicAction[] = [];

export const topicTabs: TopicTab[] = [];
