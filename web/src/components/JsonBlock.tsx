// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { Box, Code, ScrollArea } from '@mantine/core';

import { prettyJson } from '../lib/messages';
import { CopyIcon } from './CopyText';

interface JsonBlockProps {
  /** Any JSON value, or a string that is shown as is. */
  value: unknown;
  maxHeight?: number;
}

export function JsonBlock({ value, maxHeight = 360 }: JsonBlockProps) {
  const text = typeof value === 'string' ? value : prettyJson(value);
  return (
    <Box pos="relative">
      <ScrollArea.Autosize mah={maxHeight} type="auto">
        <Code block style={{ whiteSpace: 'pre-wrap', wordBreak: 'break-all' }}>
          {text}
        </Code>
      </ScrollArea.Autosize>
      <Box pos="absolute" top={4} right={4}>
        <CopyIcon value={text} />
      </Box>
    </Box>
  );
}
