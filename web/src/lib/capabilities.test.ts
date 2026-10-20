// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { describe, expect, it } from 'vitest';

import type { Capabilities, OperationType } from '../api/types';
import {
  canOperate,
  isAdmin,
  isClusterTab,
  missingCapabilities,
  operationAvailability,
  securitySections,
  visibleClusterTabs,
  withRole,
} from './capabilities';

function caps(overrides: Partial<Capabilities> = {}): Capabilities {
  return {
    manageTopics: true,
    manageConfigs: true,
    manageAcls: true,
    manageScram: true,
    manageQuotas: true,
    manageGroups: true,
    browseMessages: true,
    reassign: true,
    electLeaders: true,
    cordon: true,
    unregisterBrokers: true,
    describeQuorum: true,
    removeVoters: true,
    addControllers: false,
    provisionNodes: false,
    restartNodes: false,
    upgradeVersion: false,
    finalizeFeatures: true,
    migrateQuorum: false,
    scaleNodePools: false,
    notes: [],
    ...overrides,
  };
}

const serverTypes: OperationType[] = [
  {
    type: 'rebalance',
    title: 'Rebalance',
    description: '',
    scope: 'cluster',
    requires: ['reassign', 'electLeaders'],
    irreversible: false,
  },
  {
    type: 'drain-broker',
    title: 'Drain broker',
    description: '',
    scope: 'broker',
    requires: ['reassign', 'cordon'],
    irreversible: false,
  },
];

describe('operationAvailability', () => {
  it('allows operations whose capabilities are present', () => {
    expect(operationAvailability('rebalance', caps())).toEqual({ allowed: true });
  });

  it('explains which capability is missing', () => {
    const result = operationAvailability('add-broker', caps());
    expect(result.allowed).toBe(false);
    expect(result.reason).toContain('node restarts');
  });

  it('allows node operations on agent clusters, which manage nodes without provisioning them', () => {
    expect(operationAvailability('add-broker', caps({ restartNodes: true })).allowed).toBe(true);
    expect(operationAvailability('replace-broker', caps({ restartNodes: true })).allowed).toBe(true);
    expect(operationAvailability('destroy-cluster', caps({ restartNodes: true })).allowed).toBe(false);
    expect(operationAvailability('rolling-restart', caps()).allowed).toBe(false);
    expect(operationAvailability('rolling-restart', caps({ restartNodes: true })).allowed).toBe(true);
  });

  it('merges server requirements with the built-in ones', () => {
    expect(missingCapabilities('rebalance', caps({ electLeaders: false }))).toEqual([]);
    expect(missingCapabilities('rebalance', caps({ electLeaders: false }), serverTypes)).toEqual(['electLeaders']);
    expect(missingCapabilities('drain-broker', caps({ cordon: false }))).toEqual(['cordon']);
  });

  it('rejects operation types the server does not offer', () => {
    const result = operationAvailability('add-controller', caps({ addControllers: true }), serverTypes);
    expect(result.allowed).toBe(false);
    expect(result.reason).toContain('does not support');
  });

  it('is not allowed before capabilities are known', () => {
    expect(operationAvailability('rebalance', undefined).allowed).toBe(false);
  });
});

describe('roles', () => {
  it('lets admins and operators change clusters', () => {
    expect(canOperate('admin')).toBe(true);
    expect(canOperate('operator')).toBe(true);
    expect(canOperate('viewer')).toBe(false);
    expect(canOperate(undefined)).toBe(false);
    expect(isAdmin('operator')).toBe(false);
  });

  it('makes every action read-only for viewers', () => {
    expect(withRole({ allowed: true }, 'viewer').allowed).toBe(false);
    expect(withRole({ allowed: true }, 'operator').allowed).toBe(true);
    expect(withRole({ allowed: false, reason: 'x' }, 'admin')).toEqual({ allowed: false, reason: 'x' });
  });
});

describe('cluster tabs', () => {
  it('shows every tab for a fully managed cluster', () => {
    expect(visibleClusterTabs(caps({ upgradeVersion: true }))).toHaveLength(10);
  });

  it('hides controller, security and upgrade tabs for managed services', () => {
    const msk = caps({
      manageScram: false,
      manageQuotas: false,
      manageAcls: false,
      describeQuorum: false,
      removeVoters: false,
      finalizeFeatures: false,
    });
    const tabs = visibleClusterTabs(msk);
    expect(tabs).not.toContain('controllers');
    expect(tabs).not.toContain('security');
    expect(tabs).not.toContain('upgrade');
    expect(tabs).toContain('topics');
  });

  it('lists only the security sections the cluster supports', () => {
    expect(securitySections(caps({ manageScram: false }))).toEqual(['acls', 'quotas']);
  });

  it('recognizes tab names from the URL', () => {
    expect(isClusterTab('brokers')).toBe(true);
    expect(isClusterTab('nope')).toBe(false);
    expect(isClusterTab(undefined)).toBe(false);
  });
});
