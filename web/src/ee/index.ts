// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

import type { RouteObject } from 'react-router';

import type { DashboardEdition, TopicAction } from '../lib/edition';

export const ENTERPRISE_MARKER = 'brokerium-enterprise-dashboard-7c41e9';

export const dashboardEdition: DashboardEdition = { edition: 'enterprise', marker: ENTERPRISE_MARKER };

/** Pages of Enterprise features, inside the signed-in layout. */
export const enterpriseRoutes: RouteObject[] = [];

export const topicActions: TopicAction[] = [];
