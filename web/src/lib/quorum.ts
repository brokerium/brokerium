// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import type { Broker, Feature, QuorumReplica } from '../api/types';

/** Number of voter failures a quorum of the given size survives. */
export function faultTolerance(voters: number): number {
  return voters <= 0 ? 0 : Math.floor((voters - 1) / 2);
}

export function quorumHint(voters: number): string {
  const tolerance = faultTolerance(voters);
  const base = `${voters} ${voters === 1 ? 'voter tolerates' : 'voters tolerate'} ${tolerance} ${
    tolerance === 1 ? 'failure' : 'failures'
  }.`;
  if (voters > 0 && voters % 2 === 0) {
    return `${base} An even number of voters adds no fault tolerance over ${voters - 1}.`;
  }
  return base;
}

/** The last voter can never be removed. */
export function canRemoveVoter(voterCount: number): boolean {
  return voterCount > 1;
}

export interface VoterRemovalImpact {
  after: number;
  toleranceBefore: number;
  toleranceAfter: number;
  /** True when the removal lowers the number of tolerated failures. */
  reducesTolerance: boolean;
}

export function voterRemovalImpact(voterCount: number): VoterRemovalImpact {
  const after = Math.max(0, voterCount - 1);
  const toleranceBefore = faultTolerance(voterCount);
  const toleranceAfter = faultTolerance(after);
  return { after, toleranceBefore, toleranceAfter, reducesTolerance: toleranceAfter < toleranceBefore };
}

export type ObserverKind = 'broker' | 'controller';

export interface ClassifiedObserver extends QuorumReplica {
  kind: ObserverKind;
}

/**
 * Observers are brokers (present in the broker list) or controllers that are not voters,
 * for example a new controller waiting to be added to the quorum.
 */
export function classifyObservers(observers: QuorumReplica[], brokers: Broker[]): ClassifiedObserver[] {
  const brokerIds = new Set(brokers.map((b) => b.id));
  return observers
    .map((o) => ({ ...o, kind: (brokerIds.has(o.nodeId) ? 'broker' : 'controller') as ObserverKind }))
    .sort((a, b) => (a.kind === b.kind ? a.nodeId - b.nodeId : a.kind === 'controller' ? -1 : 1));
}

/** kraft.version 0 is a static quorum; 1 and later support dynamic membership (KIP-853). */
export function isDynamicQuorum(kraftVersion: number): boolean {
  return kraftVersion >= 1;
}

/** The finalized kraft.version, or null when the snapshot carries no feature levels. */
export function kraftVersionOf(features: readonly Feature[] | undefined): number | null {
  return features?.find((f) => f.name === 'kraft.version')?.finalized ?? null;
}

export function quorumKindLabel(kraftVersion: number | null): string {
  if (kraftVersion === null) return 'quorum type unknown';
  return isDynamicQuorum(kraftVersion) ? 'dynamic quorum' : 'static quorum';
}
