// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import type { AgentNode, BootstrapNode, NodeRole } from '../api/types';
import { quorumHint } from './quorum';

/** Role a host gets in a new cluster; unused hosts stay out of it. */
export type HostRole = NodeRole | 'unused';

/** A host in the cluster creation table. */
export interface HostPlan {
  node: AgentNode;
  role: HostRole;
  /** Explicit Kafka node id; empty for the next free id of the role. */
  nodeId: number | '';
  rack: string;
  /** Advertised host; saved on the node before planning when it changed. */
  address: string;
}

export const MAX_VOTERS = 5;
const FIRST_VOTER_ID = 1;
const FIRST_BROKER_ID = 101;

export const COMBINED_WARNING =
  'Apache Kafka recommends combined nodes only for small and test clusters: the broker and the controller share one ' +
  'process, so a busy broker slows the quorum down, and Brokerium cannot replace or drain a combined node.';

export function isVoter(role: HostRole): boolean {
  return role === 'controller' || role === 'combined';
}

export function isBroker(role: HostRole): boolean {
  return role === 'broker' || role === 'combined';
}

/** A host can join the cluster (or a new one) when it has no Kafka role and belongs to no other cluster. */
export function isFreeHost(node: AgentNode, clusterId?: string): boolean {
  return node.role === '' && (node.clusterId === null || node.clusterId === clusterId);
}

/** The address a host advertises; servers before advertised hosts only report the host name. */
export function hostAddress(node: AgentNode): string {
  return node.advertisedHost || node.hostname || node.name;
}

/** The Kafka data a host keeps from an earlier node; a new cluster cannot use it until it is cleared. */
export function hostData(node: AgentNode): string | null {
  const meta = node.status?.meta;
  if (!meta?.formatted) return null;
  return meta.clusterId ? `Kafka data of node ${meta.nodeId} of cluster ${meta.clusterId}` : 'Kafka data';
}

export function hostLabel(node: AgentNode): string {
  const address = hostAddress(node);
  return address === node.name ? node.name : `${node.name} (${address})`;
}

/** A host as it first appears in the creation table: unused, with its own rack and address. */
export function initialHostPlan(node: AgentNode): HostPlan {
  return { node, role: 'unused', nodeId: '', rack: node.rack ?? '', address: hostAddress(node) };
}

/**
 * Kafka node ids of the hosts, null for unused ones: the explicit id, else the next free id from 1 for
 * voters and from 101 for dedicated brokers, in table order.
 */
export function assignNodeIds(hosts: HostPlan[]): (number | null)[] {
  const taken = new Set<number>();
  for (const h of hosts) if (h.role !== 'unused' && h.nodeId !== '') taken.add(h.nodeId);
  const next = { voter: FIRST_VOTER_ID, broker: FIRST_BROKER_ID };
  const take = (kind: keyof typeof next): number => {
    let id = next[kind];
    while (taken.has(id)) id++;
    taken.add(id);
    next[kind] = id + 1;
    return id;
  };
  return hosts.map((h) => {
    if (h.role === 'unused') return null;
    if (h.nodeId !== '') return h.nodeId;
    return take(isVoter(h.role) ? 'voter' : 'broker');
  });
}

/** The nodes parameter of bootstrap-cluster for an agent cluster. */
export function bootstrapNodes(hosts: HostPlan[]): BootstrapNode[] {
  const ids = assignNodeIds(hosts);
  return hosts.flatMap((h, i): BootstrapNode[] => {
    const nodeId = ids[i];
    if (h.role === 'unused' || nodeId === null || nodeId === undefined) return [];
    const rack = h.rack.trim();
    return [{ node: h.node.id, role: h.role, nodeId, ...(rack ? { rack } : {}) }];
  });
}

const DNS_LABEL = /^(?!-)[A-Za-z0-9-]{1,63}(?<!-)$/;
const IPV4 = /^(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3}$/;

function isIPv6(address: string): boolean {
  if (!/^[0-9A-Fa-f:.]+$/.test(address)) return false;
  try {
    return new URL(`http://[${address}]/`).hostname !== '';
  } catch {
    return false;
  }
}

/** Error for an advertised host that is neither a DNS name nor an IP address, else null. */
export function hostAddressError(address: string): string | null {
  const a = address.trim();
  if (!a) return 'An address is required';
  if (a.includes(':')) return isIPv6(a) ? null : 'Not a valid IPv6 address';
  if (IPV4.test(a)) return null;
  if (a.length > 253) return 'Longer than 253 characters';
  return a.split('.').every((label) => DNS_LABEL.test(label)) ? null : 'Use a DNS name or an IP address';
}

export interface BootstrapCheck {
  errors: string[];
  warnings: string[];
}

/** Checks the chosen hosts before planning: errors block the plan, warnings are shown with it. */
export function checkBootstrap(hosts: HostPlan[]): BootstrapCheck {
  const chosen = hosts.filter((h) => h.role !== 'unused');
  const voters = chosen.filter((h) => isVoter(h.role)).length;
  const brokers = chosen.filter((h) => isBroker(h.role)).length;
  const errors: string[] = [];
  const warnings: string[] = [];
  if (voters === 0) errors.push('Choose at least one controller.');
  if (voters > MAX_VOTERS) errors.push(`Choose at most ${MAX_VOTERS} controllers.`);
  if (brokers === 0) errors.push('Choose at least one broker.');
  const seen = new Set<number>();
  for (const h of chosen) {
    if (h.nodeId !== '') {
      if (h.nodeId < 0 || !Number.isInteger(h.nodeId)) errors.push(`${h.node.name}: node ids are whole numbers from 0.`);
      else if (seen.has(h.nodeId)) errors.push(`Node id ${h.nodeId} is used more than once.`);
      seen.add(h.nodeId);
    }
    if (!h.node.connected) errors.push(`${h.node.name} is not connected.`);
    const data = hostData(h.node);
    if (data) errors.push(`${h.node.name} keeps ${data}: clear its data directory first.`);
    const addressError = hostAddressError(h.address);
    if (addressError) errors.push(`${h.node.name}: ${addressError}.`);
  }
  if (voters > 0 && voters <= MAX_VOTERS && voters % 2 === 0) warnings.push(quorumHint(voters));
  if (chosen.some((h) => h.role === 'combined')) warnings.push(COMBINED_WARNING);
  return { errors, warnings };
}
