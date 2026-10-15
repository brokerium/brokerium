// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { Button, Code, Group, Stack, Text, TextInput } from '@mantine/core';
import { type ReactNode, useState } from 'react';

export interface ConfirmOptions {
  title: string;
  message: ReactNode;
  confirmLabel?: string;
  /** Red confirm button; true by default because confirmations guard destructive actions. */
  danger?: boolean;
  /** When set, the user must type exactly this text (for example the cluster name). */
  typeToConfirm?: string;
  /** What the typed text is, used in the input label: "cluster name", "topic name". */
  typeToConfirmWhat?: string;
}

interface ConfirmBodyProps {
  options: ConfirmOptions;
  onConfirm: () => void;
  onCancel: () => void;
}

export function ConfirmBody({ options, onConfirm, onCancel }: ConfirmBodyProps) {
  const [typed, setTyped] = useState('');
  const expected = options.typeToConfirm;
  const ready = expected === undefined || typed === expected;
  return (
    <Stack>
      {typeof options.message === 'string' ? <Text size="sm">{options.message}</Text> : options.message}
      {expected !== undefined && (
        <TextInput
          label={
            <>
              Type the {options.typeToConfirmWhat ?? 'name'} <Code>{expected}</Code> to confirm
            </>
          }
          value={typed}
          onChange={(e) => setTyped(e.currentTarget.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && ready) onConfirm();
          }}
          autoComplete="off"
          spellCheck={false}
          data-autofocus
        />
      )}
      <Group justify="flex-end" mt="xs">
        <Button variant="default" onClick={onCancel}>
          Cancel
        </Button>
        <Button color={options.danger === false ? undefined : 'red'} disabled={!ready} onClick={onConfirm}>
          {options.confirmLabel ?? 'Confirm'}
        </Button>
      </Group>
    </Stack>
  );
}
