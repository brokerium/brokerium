// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0
//
// Checks a dashboard build against its edition: a Community build must not contain the marker
// of src/ee/index.ts, an Enterprise build must contain it.
//
//   node scripts/check-edition.mjs <community|enterprise> [dir]

import { readdirSync, readFileSync } from 'node:fs';
import { join, relative } from 'node:path';

const [edition, dir = 'dist'] = process.argv.slice(2);
if (edition !== 'community' && edition !== 'enterprise') {
  console.error('usage: node scripts/check-edition.mjs <community|enterprise> [dir]');
  process.exit(2);
}

const source = readFileSync(new URL('../src/ee/index.ts', import.meta.url), 'utf8');
const marker = source.match(/ENTERPRISE_MARKER = '([^']+)'/)?.[1];
if (!marker) {
  console.error('ENTERPRISE_MARKER is missing in src/ee/index.ts');
  process.exit(2);
}

const files = readdirSync(dir, { recursive: true, withFileTypes: true })
  .filter((entry) => entry.isFile())
  .map((entry) => join(entry.parentPath, entry.name));
const found = files.filter((file) => readFileSync(file).includes(marker));

if (edition === 'community' && found.length > 0) {
  console.error(`Enterprise code in the Community build: ${found.map((file) => relative(dir, file)).join(', ')}`);
  process.exit(1);
}
if (edition === 'enterprise' && found.length === 0) {
  console.error(`The Enterprise build in ${dir} does not contain src/ee`);
  process.exit(1);
}
console.log(`${dir}: ${files.length} files, ${edition} build as expected`);
