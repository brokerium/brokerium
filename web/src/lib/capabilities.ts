// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import type { Capabilities, OperationParams, OperationType, Role } from '../api/types';

export type CapabilityKey = Exclude<keyof Capabilities, 'notes'>;
export type OperationTypeName = keyof OperationParams;

export const CAPABILITY_LABELS: Record<CapabilityKey, string> = {
  manageTopics: 'topic management',
  manageConfigs: 'config management',
  manageAcls: 'ACL management',
  manageScram: 'SCRAM user management',
  manageQuotas: 'quota management',
  manageGroups: 'consumer group management',
  browseMessages: 'message browsing',
  reassign: 'partition reassignment',
  electLeaders: 'leader election',
  cordon: 'broker cordoning',
  unregisterBrokers: 'broker unregistration',
  describeQuorum: 'quorum description',
  removeVoters: 'voter removal',
  addControllers: 'adding controllers',
  provisionNodes: 'node provisioning',
  restartNodes: 'node restarts',
  upgradeVersion: 'version upgrades',
  finalizeFeatures: 'feature finalization',
  migrateQuorum: 'quorum migration',
  scaleNodePools: 'node pool scaling',
};

/**
 * Capabilities each operation needs, used until GET /operation-types answers and merged with
 * the server's list afterwards (the stricter union wins).
 */
export const OPERATION_REQUIREMENTS: Record<OperationTypeName, CapabilityKey[]> = {
  rebalance: ['reassign'],
  'move-partitions': ['reassign'],
  'elect-leaders': ['electLeaders'],
  'cordon-broker': ['cordon'],
  'drain-broker': ['reassign'],
  'unregister-broker': ['unregisterBrokers'],
  'add-broker': ['provisionNodes'],
  'replace-broker': ['provisionNodes'],
  'add-controller': ['addControllers'],
  'remove-controller': ['removeVoters'],
  'migrate-quorum': ['migrateQuorum'],
  'rolling-restart': ['restartNodes'],
  'rolling-upgrade': ['upgradeVersion'],
  'finalize-upgrade': ['finalizeFeatures'],
  'bootstrap-cluster': ['restartNodes'],
  'destroy-cluster': ['provisionNodes'],
  'scale-node-pool': ['scaleNodePools'],
};

export interface Availability {
  allowed: boolean;
  /** Human-readable reason when the action is not allowed. */
  reason?: string;
}

function isCapabilityKey(key: keyof Capabilities): key is CapabilityKey {
  return key !== 'notes';
}

/** Lists the capabilities required by an operation type that the cluster lacks. */
export function missingCapabilities(
  type: OperationTypeName,
  caps: Capabilities,
  serverTypes?: readonly OperationType[],
): CapabilityKey[] {
  const required = new Set<CapabilityKey>(OPERATION_REQUIREMENTS[type]);
  const server = serverTypes?.find((t) => t.type === type);
  for (const key of server?.requires ?? []) {
    if (isCapabilityKey(key)) required.add(key);
  }
  return [...required].filter((key) => !caps[key]);
}

/** Decides whether an operation can be offered for a cluster. */
export function operationAvailability(
  type: OperationTypeName,
  caps: Capabilities | undefined,
  serverTypes?: readonly OperationType[],
): Availability {
  if (!caps) return { allowed: false, reason: 'Cluster capabilities are not known yet' };
  if (serverTypes && !serverTypes.some((t) => t.type === type)) {
    return { allowed: false, reason: 'This Brokerium server does not support this operation' };
  }
  const missing = missingCapabilities(type, caps, serverTypes);
  if (missing.length > 0) {
    return {
      allowed: false,
      reason: `Not available for this cluster: requires ${missing.map((k) => CAPABILITY_LABELS[k]).join(', ')}`,
    };
  }
  return { allowed: true };
}

export function canOperate(role: Role | undefined): boolean {
  return role === 'admin' || role === 'operator';
}

export function isAdmin(role: Role | undefined): boolean {
  return role === 'admin';
}

/** Combines a capability decision with the role of the current user. */
export function withRole(availability: Availability, role: Role | undefined): Availability {
  if (!canOperate(role)) return { allowed: false, reason: 'Your role is read-only (viewer)' };
  return availability;
}

export function capabilityAvailability(caps: Capabilities | undefined, key: CapabilityKey): Availability {
  if (!caps) return { allowed: false, reason: 'Cluster capabilities are not known yet' };
  return caps[key]
    ? { allowed: true }
    : { allowed: false, reason: `Not available for this cluster: requires ${CAPABILITY_LABELS[key]}` };
}

export const CLUSTER_TABS = [
  'overview',
  'brokers',
  'controllers',
  'topics',
  'distribution',
  'groups',
  'security',
  'operations',
  'upgrade',
  'settings',
] as const;

export type ClusterTab = (typeof CLUSTER_TABS)[number];

export function isClusterTab(value: string | undefined): value is ClusterTab {
  return (CLUSTER_TABS as readonly string[]).includes(value ?? '');
}

export type SecuritySection = 'acls' | 'scram' | 'quotas';

export function securitySections(caps: Capabilities): SecuritySection[] {
  const sections: SecuritySection[] = [];
  if (caps.manageAcls) sections.push('acls');
  if (caps.manageScram) sections.push('scram');
  if (caps.manageQuotas) sections.push('quotas');
  return sections;
}

/** Decides whether a cluster tab is shown for the given capabilities. */
export function isTabVisible(tab: ClusterTab, caps: Capabilities): boolean {
  switch (tab) {
    case 'controllers':
      return caps.describeQuorum;
    case 'security':
      return securitySections(caps).length > 0;
    case 'upgrade':
      return caps.upgradeVersion || caps.finalizeFeatures;
    case 'overview':
    case 'brokers':
    case 'topics':
    case 'distribution':
    case 'groups':
    case 'operations':
    case 'settings':
      return true;
    default: {
      const exhaustive: never = tab;
      return exhaustive;
    }
  }
}

export function visibleClusterTabs(caps: Capabilities): ClusterTab[] {
  return CLUSTER_TABS.filter((tab) => isTabVisible(tab, caps));
}
