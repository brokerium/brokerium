// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { ActionIcon, CopyButton, Group, Text, type TextProps, Tooltip } from '@mantine/core';
import { IconCheck, IconCopy } from '@tabler/icons-react';

export function CopyIcon({ value, label = 'Copy' }: { value: string; label?: string }) {
  return (
    <CopyButton value={value} timeout={1500}>
      {({ copied, copy }) => (
        <Tooltip label={copied ? 'Copied' : label} withArrow>
          <ActionIcon variant="subtle" color={copied ? 'teal' : 'gray'} size="sm" onClick={copy} aria-label={label}>
            {copied ? <IconCheck size={14} /> : <IconCopy size={14} />}
          </ActionIcon>
        </Tooltip>
      )}
    </CopyButton>
  );
}

interface CopyTextProps extends TextProps {
  value: string;
  /** Visible text; defaults to the value. */
  display?: string;
}

/** Monospace value with a copy button, for ids and hostnames. */
export function CopyText({ value, display, size = 'sm', ...props }: CopyTextProps) {
  return (
    <Group gap={4} wrap="nowrap">
      <Text ff="monospace" size={size} truncate="end" title={value} {...props}>
        {display ?? value}
      </Text>
      {value && <CopyIcon value={value} />}
    </Group>
  );
}
