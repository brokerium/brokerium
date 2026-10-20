// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { Badge, type BadgeProps, Group, Loader, Tooltip } from '@mantine/core';
import { IconAlertOctagon, IconCrown } from '@tabler/icons-react';

import type {
  Broker,
  Edition,
  Flavor,
  HealthStatus,
  LicenseState,
  OperationStatus,
  Provider,
  Role,
  Severity,
} from '../api/types';
import { FLAVOR_LABELS, PROVIDER_LABELS, ROLE_COLORS, ROLE_LABELS } from '../lib/labels';
import { editionLabel, licenseStateColor, licenseStateLabel } from '../lib/license';
import { brokerState, brokerStateColor, healthColor, healthLabel, operationStatusColor, severityColor } from '../lib/status';

type SizeProps = Pick<BadgeProps, 'size'>;

export function HealthBadge({ status, size = 'sm' }: { status: HealthStatus } & SizeProps) {
  return (
    <Badge color={healthColor(status)} variant={status === 'ok' ? 'light' : 'filled'} size={size}>
      {healthLabel(status)}
    </Badge>
  );
}

export function ProviderBadge({ provider, size = 'sm' }: { provider: Provider } & SizeProps) {
  return (
    <Badge variant="outline" color="gray" size={size}>
      {PROVIDER_LABELS[provider] ?? provider}
    </Badge>
  );
}

export function FlavorBadge({ flavor, size = 'sm' }: { flavor: Flavor } & SizeProps) {
  return (
    <Badge variant="light" color="grape" size={size}>
      {FLAVOR_LABELS[flavor] ?? flavor}
    </Badge>
  );
}

export function OperationStatusBadge({ status, size = 'sm' }: { status: OperationStatus } & SizeProps) {
  return (
    <Badge
      color={operationStatusColor(status)}
      variant={status === 'running' ? 'filled' : 'light'}
      size={size}
      leftSection={status === 'running' ? <Loader size={10} color="white" /> : undefined}
    >
      {status}
    </Badge>
  );
}

export function SeverityBadge({ severity }: { severity: Severity }) {
  return (
    <Badge color={severityColor(severity)} variant="light" size="sm">
      {severity}
    </Badge>
  );
}

export function RoleBadge({ role }: { role: Role }) {
  return (
    <Badge color={ROLE_COLORS[role]} variant="light" size="sm">
      {ROLE_LABELS[role]}
    </Badge>
  );
}

export function BrokerStateBadges({ broker }: { broker: Broker }) {
  const state = brokerState(broker);
  return (
    <Group gap={4} wrap="nowrap">
      <Badge color={brokerStateColor(state)} variant={state === 'alive' ? 'light' : 'filled'} size="sm">
        {state}
      </Badge>
      {broker.cordoned && (
        <Badge color="yellow" variant="light" size="sm">
          cordoned
        </Badge>
      )}
    </Group>
  );
}

export function IrreversibleBadge({ size = 'sm' }: SizeProps) {
  return (
    <Badge color="red" variant="outline" size={size} leftSection={<IconAlertOctagon size={12} />}>
      irreversible
    </Badge>
  );
}

/** Marks a feature of Brokerium Enterprise that the license does not cover. */
export function EnterpriseBadge({ size = 'sm' }: SizeProps) {
  return (
    <Tooltip label="Needs Brokerium Enterprise and a license key that covers it" withArrow>
      <Badge color="violet" variant="light" size={size} leftSection={<IconCrown size={12} />}>
        Enterprise
      </Badge>
    </Tooltip>
  );
}

export function EditionBadge({ edition, size = 'sm' }: { edition: Edition } & SizeProps) {
  return (
    <Badge color={edition === 'enterprise' ? 'violet' : 'gray'} variant="outline" size={size}>
      {editionLabel(edition)}
    </Badge>
  );
}

export function LicenseStateBadge({ state, size = 'sm' }: { state: LicenseState } & SizeProps) {
  return (
    <Badge color={licenseStateColor(state)} variant={state === 'valid' ? 'light' : 'filled'} size={size}>
      {licenseStateLabel(state)}
    </Badge>
  );
}

export function Base64Badge() {
  return (
    <Badge color="orange" variant="light" size="xs">
      base64
    </Badge>
  );
}
