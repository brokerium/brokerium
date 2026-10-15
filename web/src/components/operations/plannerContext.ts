// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { createContext, useContext } from 'react';

import type { OperationParams } from '../../api/types';
import type { Availability, OperationTypeName } from '../../lib/capabilities';

export interface PlanOptions {
  /** Identifies the button that started planning, for its loading state; defaults to the type. */
  key?: string;
}

export interface Planner {
  /** Creates a planned operation and opens its preview; resolves to false when planning failed. */
  plan: <T extends OperationTypeName>(type: T, params: OperationParams[T], options?: PlanOptions) => Promise<boolean>;
  /** Key of the plan request in flight, if any. */
  pendingKey: string | null;
  /** Combines cluster capabilities, server support and the user's role. */
  availability: (type: OperationTypeName) => Availability;
}

export const PlannerContext = createContext<Planner | null>(null);

export function usePlanner(): Planner {
  const planner = useContext(PlannerContext);
  if (!planner) throw new Error('usePlanner must be used inside OperationPlannerProvider');
  return planner;
}
