// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import react from '@vitejs/plugin-react';
import { defineConfig } from 'vitest/config';

// Backend used by the dev server proxy; override with BROKERIUM_API=http://host:port.
const backend = process.env.BROKERIUM_API ?? 'http://localhost:8080';

export default defineConfig({
  plugins: [react()],
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
