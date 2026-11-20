// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import type { Broker, KafkaMode } from '../api/types';

export function modeLabel(mode: KafkaMode): string {
  switch (mode) {
    case 'kraft':
      return 'KRaft';
    case 'zookeeper':
      return 'ZooKeeper';
    case 'migration':
      return 'Migrating to KRaft';
    case '':
      return 'Mode unknown';
    default: {
      const exhaustive: never = mode;
      return exhaustive;
    }
  }
}

export function modeColor(mode: KafkaMode): string {
  switch (mode) {
    case 'kraft':
      return 'teal';
    case 'zookeeper':
      return 'orange';
    case 'migration':
      return 'yellow';
    case '':
      return 'gray';
    default: {
      const exhaustive: never = mode;
      return exhaustive;
    }
  }
}

export function modeDescription(mode: KafkaMode): string {
  switch (mode) {
    case 'kraft':
      return 'Every broker runs in KRaft mode.';
    case 'zookeeper':
      return 'Every broker runs in ZooKeeper mode: Brokerium watches the cluster and manages its data and partition placement; controller, node and version operations need KRaft.';
    case 'migration':
      return 'The cluster is migrating from ZooKeeper to KRaft: controller, node and version operations wait until the migration is finished.';
    case '':
      return 'No broker told its mode yet.';
    default: {
      const exhaustive: never = mode;
      return exhaustive;
    }
  }
}

/** ZooKeeper-based clusters have a controller broker instead of a KRaft quorum. */
export function isZooKeeperBased(mode: KafkaMode): boolean {
  return mode === 'zookeeper' || mode === 'migration';
}

/** The versions of a cluster in one line, for example "3.5" or "3.9, 4.0"; empty when unknown. */
export function versionsLabel(versions: readonly string[] | null | undefined): string {
  return (versions ?? []).join(', ');
}

/** The brokers of each version, for example "4.3.1 (101, 103), 4.4.0 (102)", in the order of versions. */
export function brokersByVersion(brokers: readonly Broker[], versions: readonly string[] | null | undefined): string {
  return (versions ?? [])
    .map((version) => {
      const ids = brokers.filter((b) => b.version === version).map((b) => b.id);
      return `${version} (${ids.join(', ')})`;
    })
    .join(', ');
}

/** Distinct non-empty values of a broker setting, for example the ZooKeeper connection strings. */
export function brokerSettings(brokers: readonly Broker[], pick: (b: Broker) => string | undefined): string[] {
  return [...new Set(brokers.map(pick).filter((v): v is string => !!v))].sort();
}
