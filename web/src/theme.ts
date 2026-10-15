// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { Badge, createTheme } from '@mantine/core';

export const theme = createTheme({
  primaryColor: 'indigo',
  defaultRadius: 'md',
  cursorType: 'pointer',
  headings: { fontWeight: '700' },
  components: {
    // Badges sit in table cells and nowrap groups; they must not be squeezed into an ellipsis.
    Badge: Badge.extend({ styles: { root: { flexShrink: 0, overflow: 'visible' } } }),
  },
});
