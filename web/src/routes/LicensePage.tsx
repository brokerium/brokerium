// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { Alert, Badge, Button, Code, Group, SimpleGrid, Stack, Table, Text, Textarea } from '@mantine/core';
import { IconAlertTriangle, IconExternalLink, IconKey, IconTrash } from '@tabler/icons-react';
import { type FormEvent, type ReactNode, useState } from 'react';

import { dashboardEdition } from '@ee';
import { errorMessage } from '../api/errors';
import { useDeleteLicense, useLicense, useSetLicense } from '../api/license';
import type { Edition, EditionFeature, LicenseInfo, LicenseView } from '../api/types';
import { EditionBadge, EnterpriseBadge, LicenseStateBadge } from '../components/badges';
import { confirmAction } from '../components/confirm';
import { CopyText } from '../components/CopyText';
import { notifySuccess } from '../components/notify';
import { PageHeader } from '../components/PageHeader';
import { PageLoader, QueryError } from '../components/QueryState';
import { RelativeTime } from '../components/RelativeTime';
import { usePermissions } from '../hooks/usePermissions';
import { formatDateTime } from '../lib/format';
import { ENTERPRISE_URL, canManageKey, coveredFeatures, editionLabel, licenseSourceLabel } from '../lib/license';
import { Section } from './cluster/common';

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

function StateNotice({ license }: { license: LicenseInfo }) {
  switch (license.state) {
    case 'valid':
      return null;
    case 'grace':
      return (
        <Alert color="yellow" icon={<IconAlertTriangle size={18} />} title="Grace period">
          The license expired {formatDateTime(license.expiresAt)}. Enterprise features keep working until{' '}
          {formatDateTime(license.graceEndsAt)}, <RelativeTime value={license.graceEndsAt} />. Add the renewed key before
          then.
        </Alert>
      );
    case 'expired':
      return (
        <Alert color="red" icon={<IconAlertTriangle size={18} />} title="License expired">
          The grace period ended {formatDateTime(license.graceEndsAt)}. Enterprise features are stopped; operations that
          already run can finish or be cancelled, and everything in Community keeps working.
        </Alert>
      );
    case 'invalid':
      return (
        <Alert color="red" icon={<IconAlertTriangle size={18} />} title="Invalid license key">
          Enterprise features are locked{license.error ? `: ${license.error}` : ''}.
        </Alert>
      );
    default: {
      const exhaustive: never = license.state;
      return exhaustive;
    }
  }
}

function LicenseDetails({ license, catalog }: { license: LicenseInfo; catalog: EditionFeature[] }) {
  const source = (
    <Field label="Source">
      <Text size="sm">
        {license.source === 'database' ? licenseSourceLabel(license.source) : <Code>{licenseSourceLabel(license.source)}</Code>}
      </Text>
    </Field>
  );
  if (!license.id) return source;
  return (
    <SimpleGrid cols={{ base: 1, sm: 2, lg: 3 }} spacing="md">
      <Field label="Customer">
        <Text size="sm" fw={600}>
          {license.customer}
        </Text>
        {license.email && (
          <Text size="xs" c="dimmed">
            {license.email}
          </Text>
        )}
      </Field>
      <Field label="License">
        <CopyText value={license.id} />
      </Field>
      <Field label="Features">
        <Group gap={4}>
          {coveredFeatures(license, catalog).map((title) => (
            <Badge key={title} variant="light" color="violet" size="sm">
              {title}
            </Badge>
          ))}
        </Group>
      </Field>
      <Field label="Valid from">
        <Text size="sm">{formatDateTime(license.notBefore)}</Text>
      </Field>
      <Field label="Expires">
        <Text size="sm">{formatDateTime(license.expiresAt)}</Text>
      </Field>
      <Field label="Grace period until">
        <Text size="sm">{formatDateTime(license.graceEndsAt)}</Text>
      </Field>
      {source}
    </SimpleGrid>
  );
}

function KeyForm({ license }: { license: LicenseInfo | null }) {
  const save = useSetLicense();
  const remove = useDeleteLicense();
  const [key, setKey] = useState('');

  const submit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    save.mutate(key, {
      onSuccess: (view) => {
        setKey('');
        notifySuccess('License key saved', view.license?.customer);
      },
    });
  };

  const removeKey = async () => {
    const ok = await confirmAction({
      title: 'Remove the license key',
      message: 'Enterprise features stop until a key is added again. Operations that already run can finish or be cancelled.',
      confirmLabel: 'Remove key',
    });
    if (ok) remove.mutate(undefined, { onSuccess: () => notifySuccess('License key removed') });
  };

  return (
    <form onSubmit={submit}>
      <Stack gap="sm">
        <Textarea
          label={license ? 'Replace the license key' : 'License key'}
          description="Paste the key starting with brk-lic-1. It is verified on this server, without contacting anyone."
          name="key"
          required
          autosize
          minRows={3}
          maxRows={8}
          value={key}
          onChange={(e) => {
            setKey(e.currentTarget.value);
            if (save.error) save.reset();
          }}
          error={save.error ? errorMessage(save.error) : undefined}
          autoComplete="off"
          autoCorrect="off"
          autoCapitalize="off"
          spellCheck={false}
          styles={{ input: { fontFamily: 'var(--mantine-font-family-monospace)' } }}
        />
        <Group justify="flex-end">
          {license?.source === 'database' && (
            <Button
              variant="subtle"
              color="red"
              leftSection={<IconTrash size={16} />}
              loading={remove.isPending}
              onClick={() => void removeKey()}
            >
              Remove key
            </Button>
          )}
          <Button type="submit" leftSection={<IconKey size={16} />} loading={save.isPending}>
            Save key
          </Button>
        </Group>
      </Stack>
    </form>
  );
}

function LicenseSection({ view, isAdmin }: { view: LicenseView; isAdmin: boolean }) {
  const { license } = view;
  const manage = canManageKey(view, isAdmin);
  return (
    <Section
      title="License key"
      description="Keys are verified offline; the server never reports usage."
      actions={license && <LicenseStateBadge state={license.state} size="lg" />}
    >
      <Stack gap="md">
        {license && <StateNotice license={license} />}
        {license ? (
          <LicenseDetails license={license} catalog={view.features} />
        ) : (
          <Text size="sm">There is no license key yet, so the Enterprise features below are locked. Everything in Community works without one.</Text>
        )}
        {license && license.source !== 'database' && (
          <Text size="sm" c="dimmed">
            The key comes from <Code>{licenseSourceLabel(license.source)}</Code>, which takes precedence over a key added
            here. Change it there and restart the server to replace it.
          </Text>
        )}
        {manage && <KeyForm license={license} />}
        {!isAdmin && (license === null || license.source === 'database') && (
          <Text size="sm" c="dimmed">
            Administrators add and replace the license key on this page.
          </Text>
        )}
      </Stack>
    </Section>
  );
}

function CommunitySection() {
  return (
    <Section title="Brokerium Community" description="Free under the PolyForm Shield License, including in production">
      <Stack gap="sm" align="flex-start">
        <Text size="sm">
          This server runs the Community edition. Brokerium Enterprise adds the features below on top of everything in
          Community. It runs as <Code>brokerium-server-enterprise</Code>, an image and binary next to the Community ones, and
          unlocks its features with a license key that is verified offline.
        </Text>
        <Button
          component="a"
          href={ENTERPRISE_URL}
          target="_blank"
          rel="noopener noreferrer"
          variant="light"
          rightSection={<IconExternalLink size={14} />}
        >
          About Brokerium Enterprise
        </Button>
      </Stack>
    </Section>
  );
}

function FeaturesSection({ features, edition }: { features: EditionFeature[]; edition: Edition }) {
  return (
    <Section
      title="Enterprise features"
      description={
        edition === 'community' ? 'Available in Brokerium Enterprise with a license key' : 'Unlocked by a license key that covers them'
      }
    >
      <Table.ScrollContainer minWidth={480}>
        <Table verticalSpacing="sm">
          <Table.Thead>
            <Table.Tr>
              <Table.Th>Feature</Table.Th>
              <Table.Th>Description</Table.Th>
              <Table.Th w={140}>Status</Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {features.map((f) => (
              <Table.Tr key={f.id}>
                <Table.Td>
                  <Text size="sm" fw={600}>
                    {f.title}
                  </Text>
                </Table.Td>
                <Table.Td>
                  <Text size="sm">{f.description}</Text>
                </Table.Td>
                <Table.Td>
                  {f.licensed ? (
                    <Badge color="green" variant="light" size="sm">
                      Licensed
                    </Badge>
                  ) : (
                    <EnterpriseBadge />
                  )}
                </Table.Td>
              </Table.Tr>
            ))}
          </Table.Tbody>
        </Table>
      </Table.ScrollContainer>
    </Section>
  );
}

export default function LicensePage() {
  const license = useLicense();
  const { isAdmin } = usePermissions();
  const view = license.data;

  return (
    <>
      <PageHeader
        title="License"
        description="The edition of this server and its Brokerium Enterprise license"
        badges={view && <EditionBadge edition={view.edition} size="lg" />}
      />
      {license.isPending && <PageLoader />}
      {license.error && !view && <QueryError error={license.error} onRetry={() => void license.refetch()} />}
      {view && (
        <Stack gap="md">
          {view.edition !== dashboardEdition.edition && (
            <Alert color="yellow" icon={<IconAlertTriangle size={18} />} title="Dashboard of another edition">
              The server runs the {editionLabel(view.edition)} edition, but this dashboard was built for{' '}
              {editionLabel(dashboardEdition.edition)}. Build the dashboard with <Code>VITE_EDITION={view.edition}</Code>.
            </Alert>
          )}
          {view.edition === 'community' ? <CommunitySection /> : <LicenseSection view={view} isAdmin={isAdmin} />}
          <FeaturesSection features={view.features} edition={view.edition} />
        </Stack>
      )}
    </>
  );
}
