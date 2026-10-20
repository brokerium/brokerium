// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { useLicense } from '../api/license';
import type { EditionFeature } from '../api/types';
import type { Availability } from '../lib/capabilities';
import { featureAvailability } from '../lib/license';

export interface FeatureAccess {
  /** The catalog entry; undefined while the license loads. */
  feature: EditionFeature | undefined;
  /** For GatedButton: locked features carry the reason, shown next to an EnterpriseBadge. */
  availability: Availability;
}

/** Whether the license covers an Enterprise feature. */
export function useFeature(id: string): FeatureAccess {
  const { data } = useLicense();
  return { feature: data?.features.find((f) => f.id === id), availability: featureAvailability(data, id) };
}
