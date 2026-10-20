// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { fileURLToPath } from 'node:url';

import react from '@vitejs/plugin-react';
import { defineConfig } from 'vitest/config';

// Backend used by the dev server proxy; override with BROKERIUM_API=http://host:port.
const backend = process.env.BROKERIUM_API ?? 'http://localhost:8080';

// Community builds must not contain src/ee (ADR 0016), so @ee resolves to it only for
// VITE_EDITION=enterprise.
const edition = process.env.VITE_EDITION || 'community';
if (edition !== 'community' && edition !== 'enterprise') {
  throw new Error(`VITE_EDITION must be community or enterprise, not ${edition}`);
}
const editionModule = edition === 'enterprise' ? './src/ee/index.ts' : './src/ee-community/index.ts';

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: [{ find: /^@ee$/, replacement: fileURLToPath(new URL(editionModule, import.meta.url)) }],
  },
  server: {
    port: 5173,
    strictPort: true,
    proxy: {
      '/api': { target: backend },
      '/healthz': { target: backend },
    },
  },
  preview: {
    port: 4173,
    proxy: {
      '/api': { target: backend },
      '/healthz': { target: backend },
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    target: 'es2022',
    chunkSizeWarningLimit: 700,
  },
  test: {
    environment: 'node',
    include: ['src/**/*.test.ts'],
  },
});
