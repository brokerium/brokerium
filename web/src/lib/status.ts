// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import type { Broker, HealthStatus, OperationStatus, Severity, StepStatus } from '../api/types';

export function healthColor(status: HealthStatus): string {
  switch (status) {
    case 'ok':
      return 'green';
    case 'warning':
      return 'yellow';
    case 'critical':
      return 'red';
    case 'unreachable':
    case 'unknown':
      return 'gray';
    default: {
      const exhaustive: never = status;
      return exhaustive;
    }
  }
}

export function healthLabel(status: HealthStatus): string {
  switch (status) {
    case 'ok':
      return 'Healthy';
    case 'warning':
      return 'Warning';
    case 'critical':
      return 'Critical';
    case 'unreachable':
      return 'Unreachable';
    case 'unknown':
      return 'Unknown';
    default: {
      const exhaustive: never = status;
      return exhaustive;
    }
  }
}

export function severityColor(severity: Severity): string {
  switch (severity) {
    case 'critical':
      return 'red';
    case 'warning':
      return 'yellow';
    case 'info':
      return 'blue';
    default: {
      const exhaustive: never = severity;
      return exhaustive;
    }
  }
}

const SEVERITY_ORDER: Record<Severity, number> = { critical: 0, warning: 1, info: 2 };

export function bySeverity<T extends { severity: Severity }>(a: T, b: T): number {
  return SEVERITY_ORDER[a.severity] - SEVERITY_ORDER[b.severity];
}

export function operationStatusColor(status: OperationStatus): string {
  switch (status) {
    case 'planned':
      return 'blue';
    case 'pending':
      return 'cyan';
    case 'running':
      return 'indigo';
    case 'paused':
      return 'yellow';
    case 'succeeded':
      return 'green';
    case 'failed':
      return 'red';
    case 'cancelled':
      return 'gray';
    default: {
      const exhaustive: never = status;
      return exhaustive;
    }
  }
}

/** Operations in these states change over time and are polled. */
export function isOperationActive(status: OperationStatus): boolean {
  switch (status) {
    case 'pending':
    case 'running':
    case 'paused':
      return true;
    case 'planned':
    case 'succeeded':
    case 'failed':
    case 'cancelled':
      return false;
    default: {
      const exhaustive: never = status;
      return exhaustive;
    }
  }
}

export function stepStatusColor(status: StepStatus): string {
  switch (status) {
    case 'pending':
      return 'gray';
    case 'running':
      return 'indigo';
    case 'succeeded':
      return 'green';
    case 'failed':
      return 'red';
    case 'skipped':
      return 'gray';
    default: {
      const exhaustive: never = status;
      return exhaustive;
    }
  }
}

export type BrokerState = 'alive' | 'fenced' | 'offline';

export function brokerState(broker: Broker): BrokerState {
  if (broker.fenced) return 'fenced';
  return broker.alive ? 'alive' : 'offline';
}

export function brokerStateColor(state: BrokerState): string {
  switch (state) {
    case 'alive':
      return 'green';
    case 'fenced':
      return 'red';
    case 'offline':
      return 'gray';
    default: {
      const exhaustive: never = state;
      return exhaustive;
    }
  }
}

export function diskUsageColor(percent: number): string {
  if (percent >= 90) return 'red';
  if (percent >= 75) return 'yellow';
  return 'indigo';
}
