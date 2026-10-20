// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { describe, expect, it } from 'vitest';

import type { AgentNode } from '../api/types';
import {
  COMBINED_WARNING,
  type HostPlan,
  type HostRole,
  assignNodeIds,
  bootstrapNodes,
  checkBootstrap,
  hostAddressError,
  hostLabel,
  initialHostPlan,
  isFreeHost,
} from './bootstrap';

function node(name: string, overrides: Partial<AgentNode> = {}): AgentNode {
  return {
    id: `id-${name}`,
    name,
    clusterId: null,
    role: '',
    kafkaNodeId: null,
    hostname: `${name}.local`,
    advertisedHost: `${name}.example.com`,
    rack: '',
    agentVersion: '0.3.1',
    transport: 'mtls',
    status: {} as AgentNode['status'],
    labels: null,
    providerRef: '',
    lastSeenAt: null,
    createdAt: '2026-10-21T00:00:00Z',
    connected: true,
    certNotAfter: null,
    certificateState: 'ok',
    ...overrides,
  };
}

function host(name: string, role: HostRole, overrides: Partial<HostPlan> = {}): HostPlan {
  return { ...initialHostPlan(node(name)), role, ...overrides };
}

describe('isFreeHost', () => {
  it('accepts hosts without a role in no cluster or in the given one', () => {
    expect(isFreeHost(node('a'))).toBe(true);
    expect(isFreeHost(node('a', { clusterId: 'c1' }), 'c1')).toBe(true);
    expect(isFreeHost(node('a', { clusterId: 'c1' }))).toBe(false);
    expect(isFreeHost(node('a', { clusterId: 'c2' }), 'c1')).toBe(false);
    expect(isFreeHost(node('a', { role: 'broker', clusterId: 'c1' }), 'c1')).toBe(false);
  });
});

describe('hostLabel', () => {
  it('shows the address when it differs from the name', () => {
    expect(hostLabel(node('kafka-1'))).toBe('kafka-1 (kafka-1.example.com)');
    expect(hostLabel(node('kafka-1', { advertisedHost: 'kafka-1' }))).toBe('kafka-1');
    expect(hostLabel(node('kafka-1', { advertisedHost: '' }))).toBe('kafka-1 (kafka-1.local)');
  });
});

describe('assignNodeIds', () => {
  it('numbers voters from 1 and dedicated brokers from 101 in table order', () => {
    const hosts = [host('a', 'broker'), host('b', 'controller'), host('c', 'unused'), host('d', 'combined'), host('e', 'broker')];
    expect(assignNodeIds(hosts)).toEqual([101, 1, null, 2, 102]);
  });

  it('keeps explicit ids and skips them for the others', () => {
    const hosts = [host('a', 'controller'), host('b', 'controller', { nodeId: 1 }), host('c', 'broker', { nodeId: 101 }), host('d', 'broker')];
    expect(assignNodeIds(hosts)).toEqual([2, 1, 101, 102]);
  });

  it('ignores ids typed for unused hosts', () => {
    expect(assignNodeIds([host('a', 'unused', { nodeId: 1 }), host('b', 'controller')])).toEqual([null, 1]);
  });
});

describe('bootstrapNodes', () => {
  it('lists the chosen hosts with ids and racks', () => {
    const hosts = [host('a', 'combined', { rack: ' rack-a ' }), host('b', 'unused'), host('c', 'broker')];
    expect(bootstrapNodes(hosts)).toEqual([
      { node: 'id-a', role: 'combined', nodeId: 1, rack: 'rack-a' },
      { node: 'id-c', role: 'broker', nodeId: 101 },
    ]);
  });
});

describe('hostAddressError', () => {
  it.each([
    ['kafka-1', null],
    ['kafka-1.prod.example.com', null],
    ['10.0.0.17', null],
    ['::1', null],
    ['fd00::17', null],
    ['', 'An address is required'],
    ['-kafka', 'Use a DNS name or an IP address'],
    ['kafka_1', 'Use a DNS name or an IP address'],
    ['kafka..example', 'Use a DNS name or an IP address'],
    ['kafka:9092', 'Not a valid IPv6 address'],
    [`${'a'.repeat(64)}.example`, 'Use a DNS name or an IP address'],
  ])('%s', (address, error) => {
    expect(hostAddressError(address)).toBe(error);
  });
});

describe('checkBootstrap', () => {
  it('needs a voter and a broker', () => {
    expect(checkBootstrap([]).errors).toEqual(['Choose at least one controller.', 'Choose at least one broker.']);
    expect(checkBootstrap([host('a', 'controller')]).errors).toEqual(['Choose at least one broker.']);
    expect(checkBootstrap([host('a', 'broker')]).errors).toEqual(['Choose at least one controller.']);
    expect(checkBootstrap([host('a', 'combined')]).errors).toEqual([]);
  });

  it('limits the quorum to five voters', () => {
    const hosts = ['a', 'b', 'c', 'd', 'e', 'f'].map((n) => host(n, 'controller'));
    expect(checkBootstrap([...hosts, host('g', 'broker')]).errors).toEqual(['Choose at most 5 controllers.']);
  });

  it('refuses duplicate or negative ids, disconnected hosts and bad addresses', () => {
    const hosts = [
      host('a', 'controller', { nodeId: 1 }),
      host('b', 'broker', { nodeId: 1 }),
      host('c', 'broker', { nodeId: -1 }),
      { ...host('d', 'broker'), node: node('d', { connected: false }) },
      host('e', 'broker', { address: 'not valid' }),
      host('f', 'unused', { address: '' }),
    ];
    expect(checkBootstrap(hosts).errors).toEqual([
      'Node id 1 is used more than once.',
      'c: node ids are whole numbers from 0.',
      'd is not connected.',
      'e: Use a DNS name or an IP address.',
    ]);
  });

  it('warns about an even quorum and combined nodes', () => {
    const { errors, warnings } = checkBootstrap([host('a', 'combined'), host('b', 'controller'), host('c', 'broker')]);
    expect(errors).toEqual([]);
    expect(warnings).toEqual([
      '2 voters tolerate 0 failures. An even number of voters adds no fault tolerance over 1.',
      COMBINED_WARNING,
    ]);
  });
});
