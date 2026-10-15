// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { useMe } from '../api/auth';
import type { User } from '../api/types';
import { canOperate, isAdmin } from '../lib/capabilities';

export interface Permissions {
  user: User | undefined;
  /** Admins and operators may change clusters; viewers are read-only. */
  canOperate: boolean;
  isAdmin: boolean;
}

export function usePermissions(): Permissions {
  const { data: user } = useMe();
  return { user, canOperate: canOperate(user?.role), isAdmin: isAdmin(user?.role) };
}
