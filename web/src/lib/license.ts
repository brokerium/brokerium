// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import type { Edition, EditionFeature, LicenseInfo, LicenseSource, LicenseState, LicenseView } from '../api/types';
import type { Availability } from './capabilities';

export const ENTERPRISE_URL = 'https://brokerium.kaufen/enterprise/';

/** Entry of the features of a license key that covers every feature. */
export const ALL_FEATURES = '*';

export function licenseState(license: LicenseInfo | null | undefined): LicenseState {
  return license?.state ?? 'none';
}

export function licenseStateColor(state: LicenseState): string {
  switch (state) {
    case 'valid':
      return 'green';
    case 'grace':
      return 'yellow';
    case 'expired':
    case 'invalid':
      return 'red';
    case 'none':
      return 'gray';
    default: {
      const exhaustive: never = state;
      return exhaustive;
    }
  }
}

export function licenseStateLabel(state: LicenseState): string {
  switch (state) {
    case 'valid':
      return 'Valid';
    case 'grace':
      return 'Grace period';
    case 'expired':
      return 'Expired';
    case 'invalid':
      return 'Invalid';
    case 'none':
      return 'No license';
    default: {
      const exhaustive: never = state;
      return exhaustive;
    }
  }
}

export function licenseSourceLabel(source: LicenseSource): string {
  switch (source) {
    case 'environment':
      return 'BROKERIUM_LICENSE';
    case 'file':
      return 'BROKERIUM_LICENSE_FILE';
    case 'database':
      return 'Added in the dashboard';
    default: {
      const exhaustive: never = source;
      return exhaustive;
    }
  }
}

export function editionLabel(edition: Edition): string {
  switch (edition) {
    case 'community':
      return 'Community';
    case 'enterprise':
      return 'Enterprise';
    default: {
      const exhaustive: never = edition;
      return exhaustive;
    }
  }
}

/** The license of an Enterprise server stops or has stopped its Enterprise features. */
export function needsAttention(view: LicenseView | undefined): boolean {
  if (view?.edition !== 'enterprise') return false;
  const state = licenseState(view.license);
  switch (state) {
    case 'grace':
    case 'expired':
    case 'invalid':
      return true;
    case 'valid':
    case 'none':
      return false;
    default: {
      const exhaustive: never = state;
      return exhaustive;
    }
  }
}

/** Admins of an Enterprise server manage the key in the dashboard unless it comes from the environment or a file. */
export function canManageKey(view: LicenseView, isAdmin: boolean): boolean {
  return isAdmin && view.edition === 'enterprise' && (view.license === null || view.license.source === 'database');
}

/** Titles of the features a license key lists. */
export function coveredFeatures(license: LicenseInfo, catalog: EditionFeature[]): string[] {
  if (license.features.includes(ALL_FEATURES)) return ['All Enterprise features'];
  return license.features.map((id) => catalog.find((f) => f.id === id)?.title ?? id);
}

/** Whether an Enterprise feature can be used, with the reason shown on locked controls. */
export function featureAvailability(view: LicenseView | undefined, id: string): Availability {
  if (!view) return { allowed: false, reason: 'The license is not known yet' };
  const feature = view.features.find((f) => f.id === id);
  if (feature?.licensed) return { allowed: true };
  const title = feature?.title ?? id;
  if (view.edition === 'community') return { allowed: false, reason: `${title} is a feature of Brokerium Enterprise` };
  return { allowed: false, reason: `${title} needs a Brokerium Enterprise license that covers it` };
}
