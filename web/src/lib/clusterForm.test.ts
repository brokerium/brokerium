// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { describe, expect, it } from 'vitest';

import { clusterRequestFromForm, EMPTY_CLUSTER_FORM, validateClusterForm } from './clusterForm';

describe('clusterRequestFromForm', () => {
  it('builds a plaintext request without secrets', () => {
    const req = clusterRequestFromForm({
      ...EMPTY_CLUSTER_FORM,
      name: ' alpha ',
      bootstrapServers: 'localhost:19101, localhost:19102',
      username: 'ignored',
      password: 'ignored',
    });
    expect(req).toEqual({
      name: 'alpha',
      provider: 'external',
      flavor: 'apache',
      bootstrapServers: ['localhost:19101', 'localhost:19102'],
      security: { protocol: 'PLAINTEXT' },
    });
  });

  it('sends SCRAM credentials and TLS settings for SASL_SSL', () => {
    const req = clusterRequestFromForm({
      ...EMPTY_CLUSTER_FORM,
      name: 'beta',
      bootstrapServers: 'b1:9093',
      protocol: 'SASL_SSL',
      saslMechanism: 'SCRAM-SHA-512',
      username: 'admin',
      password: 's3cret',
      tlsServerName: 'kafka.internal',
      tlsCaCertPem: '-----BEGIN CERTIFICATE-----',
    });
    expect(req.security).toEqual({
      protocol: 'SASL_SSL',
      saslMechanism: 'SCRAM-SHA-512',
      username: 'admin',
      tlsServerName: 'kafka.internal',
      tlsCaCertPem: '-----BEGIN CERTIFICATE-----',
    });
    expect(req.secrets).toEqual({ password: 's3cret' });
  });

  it('sends AWS keys only for AWS_MSK_IAM', () => {
    const req = clusterRequestFromForm({
      ...EMPTY_CLUSTER_FORM,
      name: 'msk',
      flavor: 'msk',
      bootstrapServers: 'b-1.msk:9098',
      protocol: 'SASL_SSL',
      saslMechanism: 'AWS_MSK_IAM',
      awsRegion: 'eu-central-1',
      awsAccessKeyId: 'AKIA',
      awsSecretAccessKey: 'secret',
      password: 'not sent',
    });
    expect(req.security).toEqual({ protocol: 'SASL_SSL', saslMechanism: 'AWS_MSK_IAM', awsRegion: 'eu-central-1' });
    expect(req.secrets).toEqual({ awsAccessKeyId: 'AKIA', awsSecretAccessKey: 'secret' });
  });
});

describe('validateClusterForm', () => {
  it('reports missing and invalid fields', () => {
    const errors = validateClusterForm({ ...EMPTY_CLUSTER_FORM, name: 'Bad Name', protocol: 'SASL_PLAINTEXT' });
    expect(Object.keys(errors).sort()).toEqual(['bootstrapServers', 'name', 'saslMechanism']);
  });

  it('accepts a minimal valid form', () => {
    expect(validateClusterForm({ ...EMPTY_CLUSTER_FORM, name: 'alpha', bootstrapServers: 'localhost:19101' })).toEqual({});
  });
});
