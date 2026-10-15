// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import {
  Alert,
  Button,
  Checkbox,
  Divider,
  Group,
  PasswordInput,
  Select,
  SimpleGrid,
  Stack,
  Switch,
  Text,
  Textarea,
  TextInput,
} from '@mantine/core';
import { IconAlertTriangle, IconCircleCheck, IconCircleX, IconPlugConnected } from '@tabler/icons-react';
import { useState } from 'react';

import { useTestCluster } from '../../api/clusters';
import type { ClusterRequest, Flavor, Provider, SaslMechanism, SecurityProtocol, TestResult } from '../../api/types';
import { useFormState } from '../../hooks/useFormState';
import {
  type ClusterFormValues,
  clusterRequestFromForm,
  usesPassword,
  usesSasl,
  usesTls,
  validateClusterForm,
} from '../../lib/clusterForm';
import {
  FLAVOR_LABELS,
  PROVIDER_DESCRIPTIONS,
  PROVIDER_LABELS,
  SASL_MECHANISMS,
  SECURITY_PROTOCOLS,
  selectData,
} from '../../lib/labels';

interface ClusterFormProps {
  initial: ClusterFormValues;
  mode: 'create' | 'edit';
  submitting: boolean;
  onSubmit: (request: ClusterRequest) => void;
  onCancel?: () => void;
  readOnly?: boolean;
}

const pemProps = {
  autosize: true,
  minRows: 2,
  maxRows: 8,
  styles: { input: { fontFamily: 'var(--mantine-font-family-monospace)', fontSize: 12 } },
  spellCheck: false,
} as const;

function TestResultAlert({ result }: { result: TestResult }) {
  if (result.ok) {
    return (
      <Alert color="green" icon={<IconCircleCheck size={18} />} title="Connection succeeded">
        Kafka cluster ID <b>{result.clusterId ?? 'unknown'}</b>, {result.brokers} {result.brokers === 1 ? 'broker' : 'brokers'}.
      </Alert>
    );
  }
  return (
    <Alert color="red" icon={<IconCircleX size={18} />} title="Connection failed">
      <Text size="sm" style={{ whiteSpace: 'pre-wrap' }}>
        {result.error || 'Unknown error'}
      </Text>
    </Alert>
  );
}

/** Connection settings of a cluster, used for registration and for editing. */
export function ClusterForm({ initial, mode, submitting, onSubmit, onCancel, readOnly = false }: ClusterFormProps) {
  const { values: v, set } = useFormState<ClusterFormValues>(initial);
  const [skipTest, setSkipTest] = useState(false);
  const [submitted, setSubmitted] = useState(false);
  const test = useTestCluster();
  const errors = submitted ? validateClusterForm(v) : {};
  const sasl = usesSasl(v.protocol);
  const tls = usesTls(v.protocol);
  const mechanism = sasl ? v.saslMechanism : '';
  const secretPlaceholder = mode === 'edit' ? 'Unchanged' : undefined;

  const submit = () => {
    setSubmitted(true);
    if (Object.keys(validateClusterForm(v)).length > 0) return;
    const request = clusterRequestFromForm(v);
    onSubmit(mode === 'create' && skipTest ? { ...request, skipTest: true } : request);
  };

  const runTest = () => {
    setSubmitted(true);
    if (Object.keys(validateClusterForm(v)).length > 0) return;
    test.mutate(clusterRequestFromForm(v));
  };

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        submit();
      }}
    >
      <fieldset disabled={readOnly} style={{ border: 0, padding: 0, margin: 0, minWidth: 0 }}>
        <Stack gap="sm">
          <SimpleGrid cols={{ base: 1, sm: 3 }}>
            <TextInput
              label="Name"
              description="Lowercase letters, digits and dashes"
              placeholder="production-eu"
              required
              value={v.name}
              onChange={(e) => set('name', e.currentTarget.value)}
              error={errors.name}
              data-autofocus
            />
            <Select
              label="Provider"
              description={PROVIDER_DESCRIPTIONS[v.provider]}
              data={selectData(PROVIDER_LABELS)}
              value={v.provider}
              onChange={(value) => value && set('provider', value as Provider)}
              allowDeselect={false}
            />
            <Select
              label="Flavor"
              description="Distribution or managed service"
              data={selectData(FLAVOR_LABELS)}
              value={v.flavor}
              onChange={(value) => value && set('flavor', value as Flavor)}
              allowDeselect={false}
            />
          </SimpleGrid>
          <TextInput
            label="Bootstrap servers"
            description="Comma separated host:port list"
            placeholder="kafka-1:9092, kafka-2:9092"
            required
            value={v.bootstrapServers}
            onChange={(e) => set('bootstrapServers', e.currentTarget.value)}
            error={errors.bootstrapServers}
          />

          <Divider label="Security" labelPosition="left" mt="xs" />
          <SimpleGrid cols={{ base: 1, sm: 2 }}>
            <Select
              label="Security protocol"
              data={SECURITY_PROTOCOLS}
              value={v.protocol}
              onChange={(value) => value && set('protocol', value as SecurityProtocol)}
              allowDeselect={false}
            />
            {sasl && (
              <Select
                label="SASL mechanism"
                placeholder="Select a mechanism"
                data={SASL_MECHANISMS}
                value={v.saslMechanism || null}
                onChange={(value) => set('saslMechanism', (value ?? '') as SaslMechanism | '')}
                error={errors.saslMechanism}
              />
            )}
          </SimpleGrid>

          {sasl && usesPassword(mechanism) && (
            <SimpleGrid cols={{ base: 1, sm: 2 }}>
              <TextInput
                label="Username"
                value={v.username}
                onChange={(e) => set('username', e.currentTarget.value)}
                error={errors.username}
                autoComplete="off"
              />
              <PasswordInput
                label="Password"
                placeholder={secretPlaceholder}
                value={v.password}
                onChange={(e) => set('password', e.currentTarget.value)}
                autoComplete="new-password"
              />
            </SimpleGrid>
          )}

          {mechanism === 'OAUTHBEARER' && (
            <PasswordInput
              label="OAuth bearer token"
              placeholder={secretPlaceholder}
              value={v.oauthToken}
              onChange={(e) => set('oauthToken', e.currentTarget.value)}
              autoComplete="off"
            />
          )}

          {mechanism === 'AWS_MSK_IAM' && (
            <Stack gap="sm">
              <SimpleGrid cols={{ base: 1, sm: 2 }}>
                <TextInput
                  label="AWS region"
                  placeholder="eu-central-1"
                  value={v.awsRegion}
                  onChange={(e) => set('awsRegion', e.currentTarget.value)}
                />
                <TextInput
                  label="Access key ID"
                  placeholder={secretPlaceholder ?? 'Optional: default credential chain'}
                  value={v.awsAccessKeyId}
                  onChange={(e) => set('awsAccessKeyId', e.currentTarget.value)}
                  autoComplete="off"
                />
              </SimpleGrid>
              <SimpleGrid cols={{ base: 1, sm: 2 }}>
                <PasswordInput
                  label="Secret access key"
                  placeholder={secretPlaceholder}
                  value={v.awsSecretAccessKey}
                  onChange={(e) => set('awsSecretAccessKey', e.currentTarget.value)}
                  autoComplete="off"
                />
                <PasswordInput
                  label="Session token"
                  placeholder={secretPlaceholder ?? 'Optional'}
                  value={v.awsSessionToken}
                  onChange={(e) => set('awsSessionToken', e.currentTarget.value)}
                  autoComplete="off"
                />
              </SimpleGrid>
            </Stack>
          )}

          {tls && (
            <>
              <Divider label="TLS" labelPosition="left" mt="xs" />
              <SimpleGrid cols={{ base: 1, sm: 2 }}>
                <TextInput
                  label="TLS server name"
                  description="Overrides the host name used to verify the certificate"
                  value={v.tlsServerName}
                  onChange={(e) => set('tlsServerName', e.currentTarget.value)}
                />
                <Stack gap={4} justify="flex-end">
                  <Switch
                    label="Skip certificate verification"
                    checked={v.tlsInsecureSkipVerify}
                    onChange={(e) => set('tlsInsecureSkipVerify', e.currentTarget.checked)}
                    color="red"
                  />
                  {v.tlsInsecureSkipVerify && (
                    <Text size="xs" c="red">
                      Insecure: use only for labs and tests.
                    </Text>
                  )}
                </Stack>
              </SimpleGrid>
              <Textarea
                label="CA certificate (PEM)"
                placeholder="-----BEGIN CERTIFICATE-----"
                value={v.tlsCaCertPem}
                onChange={(e) => set('tlsCaCertPem', e.currentTarget.value)}
                {...pemProps}
              />
              <SimpleGrid cols={{ base: 1, sm: 2 }}>
                <Textarea
                  label="Client certificate (PEM)"
                  placeholder={secretPlaceholder ?? '-----BEGIN CERTIFICATE-----'}
                  value={v.tlsClientCertPem}
                  onChange={(e) => set('tlsClientCertPem', e.currentTarget.value)}
                  {...pemProps}
                />
                <Textarea
                  label="Client key (PEM)"
                  placeholder={secretPlaceholder ?? '-----BEGIN PRIVATE KEY-----'}
                  value={v.tlsClientKeyPem}
                  onChange={(e) => set('tlsClientKeyPem', e.currentTarget.value)}
                  {...pemProps}
                />
              </SimpleGrid>
            </>
          )}

          {mode === 'edit' && (
            <Alert color="blue" variant="light" icon={<IconAlertTriangle size={18} />}>
              Secrets are write-only. Leave every secret field empty to keep the stored secrets. If you change any
              secret, enter all of them again: the stored set is replaced.
            </Alert>
          )}

          {test.data && <TestResultAlert result={test.data} />}

          <Group justify="space-between" mt="xs">
            <Group gap="sm">
              <Button
                variant="light"
                leftSection={<IconPlugConnected size={16} />}
                onClick={runTest}
                loading={test.isPending}
                disabled={readOnly}
              >
                Test connection
              </Button>
              {mode === 'create' && (
                <Checkbox
                  label="Register without connection test"
                  checked={skipTest}
                  onChange={(e) => setSkipTest(e.currentTarget.checked)}
                />
              )}
            </Group>
            <Group gap="xs">
              {onCancel && (
                <Button variant="default" onClick={onCancel}>
                  Cancel
                </Button>
              )}
              <Button type="submit" loading={submitting} disabled={readOnly}>
                {mode === 'create' ? 'Add cluster' : 'Save changes'}
              </Button>
            </Group>
          </Group>
          {mode === 'edit' && sasl && (
            <Text size="xs" c="dimmed">
              The connection test uses only the values in this form; enter the secrets to test an authenticated
              connection.
            </Text>
          )}
        </Stack>
      </fieldset>
    </form>
  );
}
