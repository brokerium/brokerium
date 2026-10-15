// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { createContext, useContext } from 'react';

import type { Capabilities, Cluster, ClusterView, Snapshot } from '../../api/types';

export interface ClusterContextValue {
  clusterId: string;
  view: ClusterView;
  cluster: Cluster;
  caps: Capabilities;
  /** Missing when the cluster is unreachable. */
  snapshot: Snapshot | undefined;
}

export const ClusterContext = createContext<ClusterContextValue | null>(null);

/** Data of the cluster page, provided by ClusterLayout to every tab. */
export function useClusterContext(): ClusterContextValue {
  const ctx = useContext(ClusterContext);
  if (!ctx) throw new Error('useClusterContext must be used inside ClusterLayout');
  return ctx;
}
