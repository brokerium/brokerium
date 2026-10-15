// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0
//
// Fails when a production dependency of the dashboard uses a license outside the allowlist.
// The dashboard is shipped inside the server binary, so only permissive licenses are accepted.

import { execFileSync } from 'node:child_process';

const ALLOWED = new Set([
  '0BSD',
  'Apache-2.0',
  'BlueOak-1.0.0',
  'BSD-2-Clause',
  'BSD-3-Clause',
  'CC0-1.0',
  'ISC',
  'MIT',
  'MPL-2.0',
  'Unlicense',
]);

/** Evaluates an SPDX expression such as "(MIT OR CC0-1.0)" or "MIT AND ISC". */
function allowed(expression) {
  const expr = expression.replace(/[()]/g, ' ').trim();
  return expr
    .split(/\s+OR\s+/)
    .some((alternative) => alternative.split(/\s+AND\s+/).every((id) => ALLOWED.has(id.trim())));
}

const raw = execFileSync('pnpm', ['licenses', 'list', '--prod', '--json'], { encoding: 'utf8' });
const byLicense = JSON.parse(raw);

const violations = [];
let packages = 0;
for (const [license, list] of Object.entries(byLicense)) {
  packages += list.length;
  if (!allowed(license)) {
    for (const pkg of list) violations.push(`${pkg.name}@${pkg.versions.join(',')}: ${license}`);
  }
}

if (violations.length > 0) {
  console.error(`Disallowed licenses in ${violations.length} production dependencies:`);
  for (const v of violations) console.error(`  ${v}`);
  process.exit(1);
}
console.log(`${packages} production dependencies, all licenses allowed.`);
