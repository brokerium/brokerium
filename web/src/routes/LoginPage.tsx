// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { Alert, Box, Button, Center, Paper, PasswordInput, Stack, Text, TextInput } from '@mantine/core';
import { IconAlertTriangle, IconLogin2 } from '@tabler/icons-react';
import { useQueryClient } from '@tanstack/react-query';
import { useEffect, useState } from 'react';
import { useNavigate, useSearchParams } from 'react-router';

import { authKeys, useLogin, useVersion } from '../api/auth';
import { errorMessage, isUnauthorized } from '../api/errors';
import { BrandMark } from '../components/layout/BrandMark';
import { ColorSchemeToggle } from '../components/layout/ColorSchemeToggle';
import { TrademarkNotice } from '../components/layout/TrademarkNotice';

/** Only same-origin paths are accepted as redirect targets. */
function safeNext(next: string | null): string {
  return next && next.startsWith('/') && !next.startsWith('//') ? next : '/';
}

export default function LoginPage() {
  const navigate = useNavigate();
  const [params] = useSearchParams();
  const qc = useQueryClient();
  const login = useLogin();
  const { data: version } = useVersion();
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');

  useEffect(() => {
    // Drop data of a previous session; keep only public data.
    qc.removeQueries({ predicate: (q) => q.queryKey[0] !== authKeys.version[0] });
  }, [qc]);

  const submit = () => {
    login.mutate(
      { username: username.trim(), password },
      { onSuccess: () => navigate(safeNext(params.get('next')), { replace: true }) },
    );
  };

  return (
    <Box mih="100vh" style={{ display: 'flex', flexDirection: 'column' }}>
      <Box pos="absolute" top={16} right={16}>
        <ColorSchemeToggle />
      </Box>
      <Center style={{ flex: 1 }} p="md">
        <Stack w="100%" maw={400} gap="lg">
          <Center>
            <BrandMark size="lg" />
          </Center>
          <Paper withBorder shadow="sm" p="xl" radius="md">
            <form
              onSubmit={(e) => {
                e.preventDefault();
                submit();
              }}
            >
              <Stack>
                <Text fw={600} size="lg">
                  Sign in
                </Text>
                {login.isError && (
                  <Alert color="red" icon={<IconAlertTriangle size={18} />} variant="light">
                    {isUnauthorized(login.error) ? 'Invalid username or password.' : errorMessage(login.error)}
                  </Alert>
                )}
                <TextInput
                  label="Username"
                  autoComplete="username"
                  required
                  value={username}
                  onChange={(e) => setUsername(e.currentTarget.value)}
                  data-autofocus
                  autoFocus
                />
                <PasswordInput
                  label="Password"
                  autoComplete="current-password"
                  required
                  value={password}
                  onChange={(e) => setPassword(e.currentTarget.value)}
                />
                <Button
                  type="submit"
                  fullWidth
                  loading={login.isPending}
                  leftSection={<IconLogin2 size={18} />}
                  disabled={!username.trim() || !password}
                >
                  Sign in
                </Button>
              </Stack>
            </form>
          </Paper>
          {version?.version && (
            <Text size="xs" c="dimmed" ta="center">
              Brokerium {version.version}
            </Text>
          )}
        </Stack>
      </Center>
      <TrademarkNotice p="md" />
    </Box>
  );
}
