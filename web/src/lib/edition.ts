// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0
//
// Exports of the edition module @ee: src/ee in Enterprise builds (VITE_EDITION=enterprise),
// src/ee-community otherwise. Both modules export dashboardEdition, enterpriseRoutes and
// topicActions with these types.

import type { ComponentType } from 'react';

import type { Edition } from '../api/types';

/** The edition the dashboard was built for. */
export interface DashboardEdition {
  edition: Edition;
  /** Text that only Enterprise bundles contain; scripts/check-edition.mjs looks for it. */
  marker?: string;
}

/** A tool of an Enterprise feature on the topic page. */
export interface TopicAction {
  /** Id of the Enterprise feature that the license must cover. */
  feature: string;
  label: string;
  Component: ComponentType<{ clusterId: string; topic: string }>;
}
