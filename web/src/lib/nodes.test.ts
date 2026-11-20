// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { describe, expect, it } from 'vitest';

import type { AgentNode } from '../api/types';
import { removalBlocker, uninstallCommand } from './nodes';

function node(role: AgentNode['role'], kafkaNodeId: number | null): AgentNode {
  return {
    id: 'n1',
    name: 'kafka-1',
    clusterId: role ? 'c1' : null,
    role,
    kafkaNodeId,
    hostname: 'kafka-1',
    advertisedHost: 'kafka-1',
    rack: '',
    agentVersion: '0.3.2',
    transport: 'mtls',
    status: {},
    labels: null,
    providerRef: '',
    lastSeenAt: null,
    createdAt: '2026-11-20T00:00:00Z',
    connected: false,
    certNotAfter: null,
    certificateState: '',
  };
}

describe('removalBlocker', () => {
  it('lets unassigned and combined nodes go to the server', () => {
    expect(removalBlocker(node('', null), 'prod')).toBeNull();
    expect(removalBlocker(node('combined', 1), 'prod')).toBeNull();
  });

  it('asks to decommission brokers and controllers first, connected or not', () => {
    expect(removalBlocker(node('broker', 101), 'prod')).toBe(
      'Runs broker 101 of prod. Decommission or replace it on the Brokers tab first: that stops Kafka on the host and releases it.',
    );
    expect(removalBlocker(node('controller', 1), 'prod')).toContain('Remove it from the quorum on the Controllers tab first');
  });
});

describe('uninstallCommand', () => {
  const base = 'curl -fsSL https://brokerium.example.com/api/v1/agent/uninstall.sh | sudo sh -s --';

  it('adds the chosen options', () => {
    expect(uninstallCommand(base, { stopKafka: false, removeKafka: false, deleteData: false })).toBe(base);
    expect(uninstallCommand(base, { stopKafka: true, removeKafka: false, deleteData: true })).toBe(
      `${base} --stop-kafka --delete-data`,
    );
    expect(uninstallCommand(base, { stopKafka: true, removeKafka: true, deleteData: true })).toBe(
      `${base} --stop-kafka --remove-kafka --delete-data`,
    );
  });
});
