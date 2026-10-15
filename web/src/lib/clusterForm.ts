// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import type {
  Cluster,
  ClusterRequest,
  Flavor,
  Provider,
  SaslMechanism,
  Secrets,
  Security,
  SecurityProtocol,
} from '../api/types';
import { splitList } from './format';

export interface ClusterFormValues {
  name: string;
  provider: Provider;
  flavor: Flavor;
  bootstrapServers: string;
  protocol: SecurityProtocol;
  saslMechanism: SaslMechanism | '';
  username: string;
  password: string;
  oauthToken: string;
  tlsServerName: string;
  tlsInsecureSkipVerify: boolean;
  tlsCaCertPem: string;
  tlsClientCertPem: string;
  tlsClientKeyPem: string;
  awsRegion: string;
  awsAccessKeyId: string;
  awsSecretAccessKey: string;
  awsSessionToken: string;
  /** Strimzi provider: where the Kafka resource lives and how to reach the Kubernetes API. */
  k8sNamespace: string;
  k8sKafka: string;
  k8sContext: string;
  k8sInCluster: boolean;
  kubeconfig: string;
}

export const EMPTY_CLUSTER_FORM: ClusterFormValues = {
  name: '',
  provider: 'external',
  flavor: 'apache',
  bootstrapServers: '',
  protocol: 'PLAINTEXT',
  saslMechanism: '',
  username: '',
  password: '',
  oauthToken: '',
  tlsServerName: '',
  tlsInsecureSkipVerify: false,
  tlsCaCertPem: '',
  tlsClientCertPem: '',
  tlsClientKeyPem: '',
  awsRegion: '',
  awsAccessKeyId: '',
  awsSecretAccessKey: '',
  awsSessionToken: '',
  k8sNamespace: '',
  k8sKafka: '',
  k8sContext: '',
  k8sInCluster: false,
  kubeconfig: '',
};

function settingString(settings: Record<string, unknown>, key: string): string {
  const value = settings[key];
  return typeof value === 'string' ? value : '';
}

export const CLUSTER_NAME_PATTERN = /^[a-z0-9][a-z0-9-]{0,62}$/;

export function usesTls(protocol: SecurityProtocol): boolean {
  return protocol === 'SSL' || protocol === 'SASL_SSL';
}

export function usesSasl(protocol: SecurityProtocol): boolean {
  return protocol === 'SASL_PLAINTEXT' || protocol === 'SASL_SSL';
}

/** SASL mechanisms authenticated with a username and password. */
export function usesPassword(mechanism: SaslMechanism | ''): boolean {
  return mechanism === 'PLAIN' || mechanism === 'SCRAM-SHA-256' || mechanism === 'SCRAM-SHA-512';
}

export function clusterFormFromCluster(cluster: Cluster): ClusterFormValues {
  const s = cluster.security;
  return {
    ...EMPTY_CLUSTER_FORM,
    name: cluster.name,
    provider: cluster.provider,
    flavor: cluster.flavor,
    bootstrapServers: cluster.bootstrapServers.join(', '),
    protocol: s.protocol,
    saslMechanism: s.saslMechanism ?? '',
    username: s.username ?? '',
    tlsServerName: s.tlsServerName ?? '',
    tlsInsecureSkipVerify: s.tlsInsecureSkipVerify ?? false,
    tlsCaCertPem: s.tlsCaCertPem ?? '',
    awsRegion: s.awsRegion ?? '',
    k8sNamespace: settingString(cluster.settings, 'namespace'),
    k8sKafka: settingString(cluster.settings, 'kafka'),
    k8sContext: settingString(cluster.settings, 'kubeContext'),
    k8sInCluster: cluster.settings.inCluster === true,
  };
}

/** Validates the form; returns field errors keyed by field name. */
export function validateClusterForm(v: ClusterFormValues): Partial<Record<keyof ClusterFormValues, string>> {
  const errors: Partial<Record<keyof ClusterFormValues, string>> = {};
  if (!CLUSTER_NAME_PATTERN.test(v.name.trim())) {
    errors.name = 'Lowercase letters, digits and dashes, up to 63 characters';
  }
  if (splitList(v.bootstrapServers).length === 0) {
    errors.bootstrapServers = 'At least one bootstrap server (host:port) is required';
  }
  if (usesSasl(v.protocol) && !v.saslMechanism) {
    errors.saslMechanism = `A SASL mechanism is required for ${v.protocol}`;
  }
  if (usesSasl(v.protocol) && usesPassword(v.saslMechanism) && !v.username.trim()) {
    errors.username = 'Username is required';
  }
  if (v.provider === 'strimzi') {
    if (!v.k8sNamespace.trim()) errors.k8sNamespace = 'The namespace of the Kafka resource';
    if (!v.k8sKafka.trim()) errors.k8sKafka = 'The name of the Kafka resource';
  }
  return errors;
}

/**
 * Builds the request body. Security fields not used by the chosen protocol and mechanism are
 * dropped. Secrets are sent only when at least one secret field is filled; the server replaces
 * all stored secrets when the `secrets` object is present.
 */
export function clusterRequestFromForm(v: ClusterFormValues): ClusterRequest {
  const sasl = usesSasl(v.protocol);
  const tls = usesTls(v.protocol);
  const mechanism = sasl ? v.saslMechanism : '';

  const security: Security = { protocol: v.protocol };
  const secrets: Secrets = {};
  if (sasl && mechanism) security.saslMechanism = mechanism;
  if (sasl && usesPassword(mechanism)) {
    security.username = v.username.trim();
    if (v.password) secrets.password = v.password;
  }
  if (mechanism === 'OAUTHBEARER' && v.oauthToken.trim()) secrets.oauthToken = v.oauthToken.trim();
  if (mechanism === 'AWS_MSK_IAM') {
    if (v.awsRegion.trim()) security.awsRegion = v.awsRegion.trim();
    if (v.awsAccessKeyId.trim()) secrets.awsAccessKeyId = v.awsAccessKeyId.trim();
    if (v.awsSecretAccessKey) secrets.awsSecretAccessKey = v.awsSecretAccessKey;
    if (v.awsSessionToken.trim()) secrets.awsSessionToken = v.awsSessionToken.trim();
  }
  if (tls) {
    if (v.tlsServerName.trim()) security.tlsServerName = v.tlsServerName.trim();
    if (v.tlsInsecureSkipVerify) security.tlsInsecureSkipVerify = true;
    if (v.tlsCaCertPem.trim()) security.tlsCaCertPem = v.tlsCaCertPem.trim();
    if (v.tlsClientCertPem.trim()) secrets.tlsClientCertPem = v.tlsClientCertPem.trim();
    if (v.tlsClientKeyPem.trim()) secrets.tlsClientKeyPem = v.tlsClientKeyPem.trim();
  }

  if (v.provider === 'strimzi' && !v.k8sInCluster && v.kubeconfig.trim()) secrets.kubeconfig = v.kubeconfig;

  const request: ClusterRequest = {
    name: v.name.trim(),
    provider: v.provider,
    flavor: v.flavor,
    bootstrapServers: splitList(v.bootstrapServers),
    security,
  };
  // Settings of other providers (Docker port base, versions) are owned by the server.
  if (v.provider === 'strimzi') {
    request.settings = {
      namespace: v.k8sNamespace.trim(),
      kafka: v.k8sKafka.trim(),
      kubeContext: v.k8sContext.trim(),
      inCluster: v.k8sInCluster,
    };
  }
  if (Object.keys(secrets).length > 0) request.secrets = secrets;
  return request;
}
