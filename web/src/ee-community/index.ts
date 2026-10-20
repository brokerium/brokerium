// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0
//
// The edition module of Community builds: the exports of src/ee without Enterprise features.

import type { RouteObject } from 'react-router';

import type { DashboardEdition, TopicAction, TopicTab } from '../lib/edition';

export const dashboardEdition: DashboardEdition = { edition: 'community' };

export const enterpriseRoutes: RouteObject[] = [];

export const topicActions: TopicAction[] = [];

export const topicTabs: TopicTab[] = [];
