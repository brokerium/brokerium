// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

import {
  Alert,
  Badge,
  Button,
  Code,
  Group,
  NumberInput,
  PasswordInput,
  SegmentedControl,
  SimpleGrid,
  Stack,
  Switch,
  Text,
  Textarea,
  TextInput,
} from '@mantine/core';
import { IconAlertTriangle, IconCircleCheck, IconPlugConnected, IconTrash } from '@tabler/icons-react';
import { type FormEvent, type ReactNode, useState } from 'react';
import { Link } from 'react-router';

import { errorMessage } from '../../api/errors';
import { useLicense } from '../../api/license';
import { EnterpriseBadge } from '../../components/badges';
import { confirmAction } from '../../components/confirm';
import { notifySuccess } from '../../components/notify';
import { PageHeader } from '../../components/PageHeader';
import { PageLoader, QueryError } from '../../components/QueryState';
import { RelativeTime } from '../../components/RelativeTime';
import { useFeature } from '../../hooks/useFeature';
import { useFormState } from '../../hooks/useFormState';
import { usePermissions } from '../../hooks/usePermissions';
import { Section } from '../../routes/cluster/common';
import {
  type MonitoringSettings,
  type PrometheusSource,
  type SourceAuth,
  type SourceRequest,
  type SourceTestResult,
  useDeleteSource,
  useMonitoringSettings,
  useSaveSource,
  useTestSource,
} from '../api/monitoring';
import { MONITORING } from '../features';

const AUTH: { value: SourceAuth; label: string }[] = [
  { value: 'none', label: 'None' },
  { value: 'basic', label: 'Basic' },
  { value: 'bearer', label: 'Bearer token' },
];

function authLabel(auth: SourceAuth): string {
  switch (auth) {
    case 'none':
      return 'None';
    case 'basic':
      return 'Basic authentication';
    case 'bearer':
      return 'Bearer token';
    default: {
      const exhaustive: never = auth;
      return exhaustive;
    }
  }
}

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <Stack gap={2}>
      <Text size="xs" c="dimmed" tt="uppercase" fw={600}>
        {label}
      </Text>
      {children}
    </Stack>
  );
}

function TestResultNotice({ result }: { result: SourceTestResult }) {
  if (!result.ok) {
    return (
      <Alert color="red" icon={<IconAlertTriangle size={18} />} title="The test failed">
        {result.error}
      </Alert>
    );
  }
  return (
    <Alert color="green" icon={<IconCircleCheck size={18} />} title="Prometheus answered">
      {result.version ? `Version ${result.version}, ` : ''}
      {result.durationMs} ms for a query.
    </Alert>
  );
}

interface FormValues {
  url: string;
  auth: SourceAuth;
  username: string;
  password: string;
  token: string;
  caPem: string;
  insecureSkipVerify: boolean;
  timeoutSeconds: number | string;
}

function formValues(source: PrometheusSource): FormValues {
  return {
    url: source.url,
    auth: source.auth,
    username: source.username,
    password: '',
    token: '',
    caPem: source.caPem,
    insecureSkipVerify: source.insecureSkipVerify,
    timeoutSeconds: source.timeoutSeconds,
  };
}

/** The request of the form; empty secrets keep the stored ones. */
function sourceRequest(v: FormValues): SourceRequest {
  return {
    url: v.url.trim(),
    auth: v.auth,
    username: v.auth === 'basic' ? v.username.trim() : undefined,
    password: v.auth === 'basic' && v.password !== '' ? v.password : undefined,
    token: v.auth === 'bearer' && v.token.trim() !== '' ? v.token.trim() : undefined,
    caPem: v.caPem.trim(),
    insecureSkipVerify: v.insecureSkipVerify,
    timeoutSeconds: typeof v.timeoutSeconds === 'number' ? v.timeoutSeconds : 0,
  };
}

function SourceForm({ source }: { source: PrometheusSource }) {
  const form = useFormState<FormValues>(formValues(source));
  const save = useSaveSource();
  const remove = useDeleteSource();
  const test = useTestSource();
  const [result, setResult] = useState<SourceTestResult | null>(null);
  const v = form.values;
  const storedSecret = (set: boolean, auth: SourceAuth) => set && source.auth === auth;

  const submit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    setResult(null);
    save.mutate(sourceRequest(v), { onSuccess: (settings) => notifySuccess('Data source saved', settings.source.url) });
  };

  const removeSource = async () => {
    const ok = await confirmAction({
      title: 'Remove the data source',
      message: 'The Monitoring tab of every cluster shows only the values Brokerium samples until a data source is set again.',
      confirmLabel: 'Remove',
    });
    if (ok) {
      remove.mutate(undefined, {
        onSuccess: () => {
          setResult(null);
          notifySuccess('Data source removed');
        },
      });
    }
  };

  return (
    <form onSubmit={submit}>
      <Stack gap="md">
        <TextInput
          label="URL"
          description="Base URL of the Prometheus HTTP API, with the path prefix of Thanos, Mimir or VictoriaMetrics if any"
          placeholder="http://prometheus.monitoring.svc:9090"
          required
          value={v.url}
          onChange={(e) => form.set('url', e.currentTarget.value)}
          autoComplete="off"
          spellCheck={false}
        />
        <Stack gap={4}>
          <Text size="sm" fw={500}>
            Authentication
          </Text>
          <SegmentedControl data={AUTH} value={v.auth} onChange={(auth) => form.set('auth', auth)} w="fit-content" />
        </Stack>
        {v.auth === 'basic' && (
          <SimpleGrid cols={{ base: 1, sm: 2 }}>
            <TextInput
              label="Username"
              required
              value={v.username}
              onChange={(e) => form.set('username', e.currentTarget.value)}
              autoComplete="off"
            />
            <PasswordInput
              label="Password"
              placeholder={storedSecret(source.passwordSet, 'basic') ? 'Stored; leave empty to keep it' : undefined}
              value={v.password}
              onChange={(e) => form.set('password', e.currentTarget.value)}
              autoComplete="new-password"
            />
          </SimpleGrid>
        )}
        {v.auth === 'bearer' && (
          <PasswordInput
            label="Token"
            required={!storedSecret(source.tokenSet, 'bearer')}
            placeholder={storedSecret(source.tokenSet, 'bearer') ? 'Stored; leave empty to keep it' : undefined}
            value={v.token}
            onChange={(e) => form.set('token', e.currentTarget.value)}
            autoComplete="new-password"
          />
        )}
        <Textarea
          label="CA certificate"
          description="PEM certificates that sign the certificate of Prometheus, when the system CAs do not"
          placeholder="-----BEGIN CERTIFICATE-----"
          autosize
          minRows={2}
          maxRows={8}
          value={v.caPem}
          onChange={(e) => form.set('caPem', e.currentTarget.value)}
          spellCheck={false}
          styles={{ input: { fontFamily: 'var(--mantine-font-family-monospace)' } }}
        />
        <Group gap="xl" align="flex-end">
          <NumberInput
            label="Timeout"
            description="Seconds per query"
            min={1}
            max={300}
            allowDecimal={false}
            value={v.timeoutSeconds}
            onChange={(value) => form.set('timeoutSeconds', value)}
            w={160}
          />
          <Switch
            label="Skip TLS verification"
            description="Accepts any certificate; use a CA certificate instead where you can"
            checked={v.insecureSkipVerify}
            onChange={(e) => form.set('insecureSkipVerify', e.currentTarget.checked)}
          />
        </Group>
        {save.error && (
          <Alert color="red" icon={<IconAlertTriangle size={18} />} title="Could not save the data source">
            {errorMessage(save.error)}
          </Alert>
        )}
        {result && <TestResultNotice result={result} />}
        <Group justify="space-between">
          <div>
            {source.configured && (
              <Button
                variant="subtle"
                color="red"
                leftSection={<IconTrash size={16} />}
                loading={remove.isPending}
                onClick={() => void removeSource()}
              >
                Remove
              </Button>
            )}
          </div>
          <Group gap="sm">
            <Button
              variant="default"
              leftSection={<IconPlugConnected size={16} />}
              loading={test.isPending}
              disabled={v.url.trim() === ''}
              onClick={() => test.mutate(sourceRequest(v), { onSuccess: setResult })}
            >
              Test
            </Button>
            <Button type="submit" loading={save.isPending}>
              Save
            </Button>
          </Group>
        </Group>
      </Stack>
    </form>
  );
}

function EnvironmentSource({ source }: { source: PrometheusSource }) {
  const test = useTestSource();
  const [result, setResult] = useState<SourceTestResult | null>(null);
  return (
    <Stack gap="md">
      <Text size="sm">
        The data source comes from <Code>BROKERIUM_PROMETHEUS_URL</Code>, which takes precedence over one set here. Change the
        environment of the server and restart it to replace it.
      </Text>
      <SimpleGrid cols={{ base: 1, sm: 2, lg: 3 }} spacing="md">
        <Field label="URL">
          <Text size="sm" ff="monospace" style={{ wordBreak: 'break-all' }}>
            {source.url}
          </Text>
        </Field>
        <Field label="Authentication">
          <Text size="sm">
            {authLabel(source.auth)}
            {source.username ? ` as ${source.username}` : ''}
          </Text>
        </Field>
        <Field label="CA certificate">
          <Text size="sm">{source.caPem ? 'From BROKERIUM_PROMETHEUS_CA_FILE' : 'System CAs'}</Text>
        </Field>
      </SimpleGrid>
      {source.error && (
        <Alert color="red" icon={<IconAlertTriangle size={18} />} title="The data source cannot be used">
          {source.error}
        </Alert>
      )}
      {result && <TestResultNotice result={result} />}
      <Group justify="flex-end">
        <Button
          variant="default"
          leftSection={<IconPlugConnected size={16} />}
          loading={test.isPending}
          onClick={() => test.mutate({}, { onSuccess: setResult })}
        >
          Test
        </Button>
      </Group>
    </Stack>
  );
}

function SourceSection({ settings }: { settings: MonitoringSettings }) {
  const { source } = settings;
  return (
    <Section
      title="Prometheus"
      description="The Monitoring tab of every cluster queries it; the browser never contacts it."
      actions={
        source.configured ? (
          <Badge variant="light" color="green">
            Configured
          </Badge>
        ) : (
          <Badge variant="light" color="gray">
            Not configured
          </Badge>
        )
      }
    >
      {source.origin === 'environment' ? (
        <EnvironmentSource source={source} />
      ) : (
        <Stack gap="sm">
          {source.updatedAt && (
            <Text size="xs" c="dimmed">
              Saved by {source.updatedBy} <RelativeTime value={source.updatedAt} size="xs" c="dimmed" />
            </Text>
          )}
          {source.error && (
            <Alert color="red" icon={<IconAlertTriangle size={18} />} title="The stored data source cannot be used">
              {source.error}
            </Alert>
          )}
          <SourceForm key={source.updatedAt ?? 'new'} source={source} />
        </Stack>
      )}
    </Section>
  );
}

function MetricsSection({ settings }: { settings: MonitoringSettings }) {
  const endpoint = settings.metricsEndpoint;
  return (
    <Section
      title="Metrics endpoint"
      description="GET /metrics: health, lag, traffic estimates and disk usage of every cluster for your Prometheus"
      actions={
        <Badge variant="light" color={endpoint.enabled ? 'green' : 'gray'}>
          {endpoint.enabled ? 'On' : 'Off'}
        </Badge>
      }
    >
      <Stack gap="xs">
        {endpoint.error && (
          <Alert color="red" icon={<IconAlertTriangle size={18} />} title="The token cannot be read">
            {endpoint.error}
          </Alert>
        )}
        {endpoint.enabled ? (
          <Text size="sm">
            Prometheus scrapes <Code>/metrics</Code> with the token of{' '}
            <Code>{endpoint.tokenSource === 'file' ? 'BROKERIUM_METRICS_TOKEN_FILE' : 'BROKERIUM_METRICS_TOKEN'}</Code> as a
            bearer token.
          </Text>
        ) : (
          <Text size="sm">
            Set <Code>BROKERIUM_METRICS_TOKEN</Code> or <Code>BROKERIUM_METRICS_TOKEN_FILE</Code> on the server (Helm:{' '}
            <Code>metrics.enabled</Code>) to turn it on; without a token it answers 404.
          </Text>
        )}
      </Stack>
    </Section>
  );
}

export default function MonitoringSettingsPage() {
  const license = useLicense();
  const { availability } = useFeature(MONITORING);
  const { isAdmin } = usePermissions();
  const settings = useMonitoringSettings(availability.allowed && isAdmin);

  let content: ReactNode;
  if (license.isPending) {
    content = <PageLoader />;
  } else if (!availability.allowed) {
    content = (
      <Alert
        title={
          <Group gap="xs">
            Monitoring <EnterpriseBadge size="xs" />
          </Group>
        }
      >
        <Text size="sm">
          {availability.reason}. See <Link to="/license">License</Link> for the features of Brokerium Enterprise.
        </Text>
      </Alert>
    );
  } else if (!isAdmin) {
    content = (
      <Alert color="gray" title="Administrators only">
        Administrators set the Prometheus data source. The Monitoring tab of every cluster shows its charts.
      </Alert>
    );
  } else if (settings.isPending) {
    content = <PageLoader />;
  } else if (!settings.data) {
    content = <QueryError error={settings.error} onRetry={() => void settings.refetch()} />;
  } else {
    content = (
      <Stack gap="md">
        <SourceSection settings={settings.data} />
        <MetricsSection settings={settings.data} />
        <Text size="sm" c="dimmed">
          Each cluster finds its metrics with a selector, by default the exporters on the hosts of its brokers; administrators
          change it on the Monitoring tab of the cluster.
        </Text>
      </Stack>
    );
  }

  return (
    <>
      <PageHeader title="Monitoring" description="The Prometheus behind the Monitoring tab, and the metrics endpoint of the server" />
      {content}
    </>
  );
}
