// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { describe, expect, it } from 'vitest';

import type { Broker } from '../api/types';
import { brokerSettings, brokersByVersion, isZooKeeperBased, modeLabel, versionsLabel } from './kafkaMode';

function broker(id: number, version: string, extra: Partial<Broker> = {}): Broker {
  return {
    id,
    host: `b${id}`,
    port: 9092,
    rack: '',
    fenced: false,
    alive: true,
    cordoned: false,
    logDirs: [],
    diskTotalBytes: 0,
    diskUsableBytes: 0,
    dataBytes: 0,
    replicas: 0,
    leaders: 0,
    preferredLeaders: 0,
    version,
    versionExact: false,
    mode: 'zookeeper',
    ...extra,
  };
}

describe('Kafka modes', () => {
  it('names the modes', () => {
    expect(modeLabel('kraft')).toBe('KRaft');
    expect(modeLabel('zookeeper')).toBe('ZooKeeper');
    expect(modeLabel('migration')).toBe('Migrating to KRaft');
  });

  it('tells which modes have a controller broker', () => {
    expect(isZooKeeperBased('zookeeper')).toBe(true);
    expect(isZooKeeperBased('migration')).toBe(true);
    expect(isZooKeeperBased('kraft')).toBe(false);
    expect(isZooKeeperBased('')).toBe(false);
  });
});

describe('versions', () => {
  it('lists the versions of a cluster', () => {
    expect(versionsLabel(['3.9', '4.0'])).toBe('3.9, 4.0');
    expect(versionsLabel(null)).toBe('');
  });

  it('names the brokers of each version', () => {
    const brokers = [broker(101, '4.3.1'), broker(102, '4.4.0'), broker(103, '4.3.1')];
    expect(brokersByVersion(brokers, ['4.3.1', '4.4.0'])).toBe('4.3.1 (101, 103), 4.4.0 (102)');
  });

  it('collects distinct broker settings', () => {
    const brokers = [
      broker(101, '3.5', { zookeeperConnect: 'zk:2181' }),
      broker(102, '3.5', { zookeeperConnect: 'zk:2181' }),
      broker(103, '3.5'),
    ];
    expect(brokerSettings(brokers, (b) => b.zookeeperConnect)).toEqual(['zk:2181']);
  });
});
