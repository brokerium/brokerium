// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

import type { RouteObject } from 'react-router';

import type { DashboardEdition, TopicAction, TopicTab } from '../lib/edition';
import { MESSAGE_TOOLS } from './features';
import { CopyMessagesAction } from './topic/CopyMessagesAction';
import { DeleteRecordsAction } from './topic/DeleteRecordsAction';
import { SearchPanel } from './topic/SearchPanel';

export const ENTERPRISE_MARKER = 'brokerium-enterprise-dashboard-7c41e9';

export const dashboardEdition: DashboardEdition = { edition: 'enterprise', marker: ENTERPRISE_MARKER };

/** Pages of Enterprise features, inside the signed-in layout. */
export const enterpriseRoutes: RouteObject[] = [];

export const topicActions: TopicAction[] = [
  { feature: MESSAGE_TOOLS, label: 'Copy messages', Component: CopyMessagesAction },
  { feature: MESSAGE_TOOLS, label: 'Delete records', Component: DeleteRecordsAction },
];

export const topicTabs: TopicTab[] = [{ feature: MESSAGE_TOOLS, value: 'search', label: 'Search', Component: SearchPanel }];
