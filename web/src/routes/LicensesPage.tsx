// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { Code, Paper, Tabs, Text } from '@mantine/core';
import { useState } from 'react';

import { useNotices } from '../api/server';
import { PageHeader } from '../components/PageHeader';
import { PageLoader, QueryError } from '../components/QueryState';

export default function LicensesPage() {
  const notices = useNotices();
  const files = notices.data?.files;
  const [active, setActive] = useState<string | null>(null);

  return (
    <>
      <PageHeader
        title="Licenses"
        description="The license of Brokerium and the licenses and notices of the third-party software it includes"
      />
      {notices.isPending && <PageLoader />}
      {notices.error && !files && <QueryError error={notices.error} onRetry={() => void notices.refetch()} />}
      {files && files.length > 0 && (
        <Paper withBorder radius="md" p="md">
          <Tabs value={active ?? files[0].name} onChange={setActive} keepMounted={false} variant="pills">
            <Tabs.List mb="md">
              {files.map((f) => (
                <Tabs.Tab key={f.name} value={f.name}>
                  {f.title}
                </Tabs.Tab>
              ))}
            </Tabs.List>
            {files.map((f) => (
              <Tabs.Panel key={f.name} value={f.name}>
                <Text size="sm" c="dimmed" mb="sm">
                  <Code>{f.name}</Code> also ships in <Code>/licenses</Code> of the container images and with every
                  release.
                </Text>
                <Code block style={{ whiteSpace: 'pre-wrap', wordBreak: 'break-word' }}>
                  {f.text}
                </Code>
              </Tabs.Panel>
            ))}
          </Tabs>
        </Paper>
      )}
    </>
  );
}
