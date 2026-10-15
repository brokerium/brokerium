// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import type { Flavor, Provider, Role, SaslMechanism, SecurityProtocol } from '../api/types';

export const PROVIDER_LABELS: Record<Provider, string> = {
  external: 'External',
  agent: 'Agent',
  docker: 'Docker',
  strimzi: 'Strimzi',
};

export const PROVIDER_DESCRIPTIONS: Record<Provider, string> = {
  external: 'Existing cluster reached over the Kafka protocol only',
  agent: 'Nodes run brokerium-agent on their hosts',
  docker: 'Lab nodes managed through Docker',
  strimzi: 'Cluster managed by the Strimzi operator on Kubernetes',
};

export const FLAVOR_LABELS: Record<Flavor, string> = {
  apache: 'Apache Kafka',
  msk: 'Amazon MSK',
  'confluent-cloud': 'Confluent Cloud',
  aiven: 'Aiven',
};

export const ROLE_LABELS: Record<Role, string> = {
  admin: 'Admin',
  operator: 'Operator',
  viewer: 'Viewer',
};

export const ROLE_COLORS: Record<Role, string> = {
  admin: 'red',
  operator: 'indigo',
  viewer: 'gray',
};

export const SECURITY_PROTOCOLS: SecurityProtocol[] = ['PLAINTEXT', 'SSL', 'SASL_PLAINTEXT', 'SASL_SSL'];

export const SASL_MECHANISMS: SaslMechanism[] = ['PLAIN', 'SCRAM-SHA-256', 'SCRAM-SHA-512', 'AWS_MSK_IAM', 'OAUTHBEARER'];

export function selectData<T extends string>(labels: Record<T, string>): { value: T; label: string }[] {
  return (Object.keys(labels) as T[]).map((value) => ({ value, label: labels[value] }));
}
