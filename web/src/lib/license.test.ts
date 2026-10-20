// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { describe, expect, it } from 'vitest';

import type { LicenseInfo, LicenseView } from '../api/types';
import {
  canManageKey,
  coveredFeatures,
  featureAvailability,
  licenseSourceLabel,
  licenseState,
  licenseStateColor,
  licenseStateLabel,
  needsAttention,
} from './license';

const catalog = [{ id: 'message-tools', title: 'Message tools', description: 'Search messages.', licensed: false }];

function license(overrides: Partial<LicenseInfo> = {}): LicenseInfo {
  return {
    id: 'lic_1',
    customer: 'Example GmbH',
    email: 'ops@example.com',
    features: ['message-tools'],
    expiresAt: '2027-10-20T00:00:00Z',
    graceEndsAt: '2027-11-03T00:00:00Z',
    state: 'valid',
    source: 'database',
    ...overrides,
  };
}

function view(overrides: Partial<LicenseView> = {}): LicenseView {
  return { edition: 'enterprise', features: catalog, license: license(), ...overrides };
}

describe('license states', () => {
  it('treats a missing key as none', () => {
    expect(licenseState(null)).toBe('none');
    expect(licenseState(undefined)).toBe('none');
    expect(licenseState(license({ state: 'grace' }))).toBe('grace');
  });

  it('colors and labels every state', () => {
    expect(licenseStateColor('valid')).toBe('green');
    expect(licenseStateColor('grace')).toBe('yellow');
    expect(licenseStateColor('expired')).toBe('red');
    expect(licenseStateColor('invalid')).toBe('red');
    expect(licenseStateColor('none')).toBe('gray');
    expect(licenseStateLabel('grace')).toBe('Grace period');
    expect(licenseStateLabel('none')).toBe('No license');
  });

  it('names the source of the key', () => {
    expect(licenseSourceLabel('environment')).toBe('BROKERIUM_LICENSE');
    expect(licenseSourceLabel('file')).toBe('BROKERIUM_LICENSE_FILE');
    expect(licenseSourceLabel('database')).toBe('Added in the dashboard');
  });

  it('asks for attention when Enterprise features stop', () => {
    expect(needsAttention(view({ license: license({ state: 'grace' }) }))).toBe(true);
    expect(needsAttention(view({ license: license({ state: 'expired' }) }))).toBe(true);
    expect(needsAttention(view({ license: license({ state: 'invalid' }) }))).toBe(true);
    expect(needsAttention(view())).toBe(false);
    expect(needsAttention(view({ license: null }))).toBe(false);
    expect(needsAttention(view({ edition: 'community', license: null }))).toBe(false);
    expect(needsAttention(undefined)).toBe(false);
  });
});

describe('managing the key', () => {
  it('lets admins of Enterprise servers manage keys stored in the database', () => {
    expect(canManageKey(view(), true)).toBe(true);
    expect(canManageKey(view({ license: null }), true)).toBe(true);
    expect(canManageKey(view(), false)).toBe(false);
    expect(canManageKey(view({ license: license({ source: 'environment' }) }), true)).toBe(false);
    expect(canManageKey(view({ license: license({ source: 'file' }) }), true)).toBe(false);
    expect(canManageKey(view({ edition: 'community', license: null }), true)).toBe(false);
  });

  it('lists the features of a key by title', () => {
    expect(coveredFeatures(license(), catalog)).toEqual(['Message tools']);
    expect(coveredFeatures(license({ features: ['*'] }), catalog)).toEqual(['All Enterprise features']);
    expect(coveredFeatures(license({ features: ['future-feature'] }), catalog)).toEqual(['future-feature']);
  });
});

describe('feature availability', () => {
  it('allows licensed features only', () => {
    const licensed = view({ features: [{ ...catalog[0], licensed: true }] });
    expect(featureAvailability(licensed, 'message-tools')).toEqual({ allowed: true });
    expect(featureAvailability(view(), 'message-tools')).toEqual({
      allowed: false,
      reason: 'Message tools needs a Brokerium Enterprise license that covers it',
    });
  });

  it('explains Enterprise features in Community', () => {
    expect(featureAvailability(view({ edition: 'community', license: null }), 'message-tools')).toEqual({
      allowed: false,
      reason: 'Message tools is a feature of Brokerium Enterprise',
    });
    expect(featureAvailability(undefined, 'message-tools').allowed).toBe(false);
  });
});
