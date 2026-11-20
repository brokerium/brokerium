// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import type { AgentNode } from '../api/types';

/**
 * Why a node cannot be removed yet, or null. Brokers and controllers are decommissioned first;
 * the server removes a combined node, which Brokerium does not decommission, once it left its
 * cluster.
 */
export function removalBlocker(node: AgentNode, cluster: string): string | null {
  switch (node.role) {
    case '':
    case 'combined':
      return null;
    case 'broker':
      return `Runs broker ${node.kafkaNodeId} of ${cluster}. Decommission or replace it on the Brokers tab first: that stops Kafka on the host and releases it.`;
    case 'controller':
      return `Runs controller ${node.kafkaNodeId} of ${cluster}. Remove it from the quorum on the Controllers tab first: that stops Kafka on the host and releases it.`;
    default: {
      const unhandled: never = node.role;
      return unhandled;
    }
  }
}

/** What the uninstall script removes besides the agent. */
export interface UninstallOptions {
  stopKafka: boolean;
  removeKafka: boolean;
  deleteData: boolean;
}

/** The uninstall command from the server with the options of the script. */
export function uninstallCommand(command: string, options: UninstallOptions): string {
  const flags = [
    options.stopKafka ? '--stop-kafka' : '',
    options.removeKafka ? '--remove-kafka' : '',
    options.deleteData ? '--delete-data' : '',
  ].filter((flag) => flag !== '');
  return [command, ...flags].join(' ');
}
