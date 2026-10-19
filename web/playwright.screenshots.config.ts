// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0
import { defineConfig } from '@playwright/test';

// Used by deploy/lab/screenshots.sh against a server with demo data.
export default defineConfig({
  testDir: 'screenshots',
  timeout: 120_000,
  workers: 1,
  reporter: 'list',
  use: {
    baseURL: process.env.BROKERIUM_URL ?? 'http://localhost:18090',
    viewport: { width: 1440, height: 900 },
    deviceScaleFactor: 2,
  },
  projects: [
    { name: 'light', use: { colorScheme: 'light' } },
    { name: 'dark', use: { colorScheme: 'dark' } },
  ],
});
