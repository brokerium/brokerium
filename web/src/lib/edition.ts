// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0
//
// Exports of the edition module @ee: src/ee in Enterprise builds (VITE_EDITION=enterprise),
// src/ee-community otherwise. Both modules export dashboardEdition, enterpriseRoutes,
// navExtensions, clusterTabs, overviewColumns, topicActions and topicTabs with these types.

import type { ComponentType } from 'react';

import type { Edition } from '../api/types';

/** The edition the dashboard was built for. */
export interface DashboardEdition {
  edition: Edition;
  /** Text that only Enterprise bundles contain; scripts/check-edition.mjs looks for it. */
  marker?: string;
}

/** A page of an Enterprise feature in the sidebar; its route is one of enterpriseRoutes. */
export interface NavExtension {
  /** Id of the Enterprise feature that the license must cover. */
  feature: string;
  to: string;
  label: string;
  icon: ComponentType<{ size?: number; stroke?: number }>;
  adminOnly?: boolean;
}

/** A tab of an Enterprise feature on the cluster page, shown after the Overview tab. */
export interface ClusterTabExtension {
  /** Id of the Enterprise feature that the license must cover. */
  feature: string;
  /** Path segment of the tab below /clusters/:clusterId. */
  value: string;
  label: string;
  icon: ComponentType<{ size?: number }>;
  /** Loads the page of the tab; it reads the cluster from the cluster context. */
  load: () => Promise<{ default: ComponentType }>;
}

/** A column of an Enterprise feature in the cluster table of the overview page, before Top issue. */
export interface OverviewColumn {
  /** Id of the Enterprise feature that the license must cover. */
  feature: string;
  header: string;
  Cell: ComponentType<{ clusterId: string }>;
}

/** A tool of an Enterprise feature on the topic page. */
export interface TopicAction {
  /** Id of the Enterprise feature that the license must cover. */
  feature: string;
  label: string;
  Component: ComponentType<{ clusterId: string; topic: string }>;
}

/** A tab of an Enterprise feature on the topic page. */
export interface TopicTab {
  /** Id of the Enterprise feature that the license must cover. */
  feature: string;
  value: string;
  label: string;
  Component: ComponentType<{ clusterId: string; topic: string; partitionCount: number }>;
}
