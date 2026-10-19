// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0
import { expect, test, type Page, type TestInfo } from '@playwright/test';

const headers = { 'X-Brokerium-Request': '1' };

interface ClusterView {
  cluster: { id: string; name: string };
}

async function login(page: Page) {
  const response = await page.request.post('/api/v1/auth/login', {
    headers,
    data: { username: 'admin', password: process.env.BROKERIUM_PASSWORD ?? 'screenshots' },
  });
  expect(response.ok()).toBe(true);
}

async function clusterId(page: Page, name: string) {
  const body = (await (await page.request.get('/api/v1/clusters', { headers })).json()) as
    | ClusterView[]
    | { clusters: ClusterView[] };
  const views = Array.isArray(body) ? body : body.clusters;
  const view = views.find((v) => v.cluster.name === name);
  if (!view) throw new Error(`cluster ${name} is not registered`);
  return view.cluster.id;
}

async function capture(page: Page, info: TestInfo, name: string) {
  await page.waitForLoadState('networkidle');
  await page.waitForTimeout(1500);
  await page.screenshot({ path: `../docs/images/${name}-${info.project.name}.png` });
}

test.beforeEach(async ({ page }) => {
  await login(page);
});

test('overview', async ({ page }, info) => {
  await page.goto('/');
  await expect(page.getByText('alpha').first()).toBeVisible();
  await expect(page.getByText('beta').first()).toBeVisible();
  await capture(page, info, 'overview');
});

for (const tab of ['brokers', 'controllers', 'distribution']) {
  test(tab, async ({ page }, info) => {
    await page.goto(`/clusters/${await clusterId(page, 'alpha')}/${tab}`);
    await capture(page, info, tab);
  });
}

test('rebalance plan', async ({ page }, info) => {
  const alpha = await clusterId(page, 'alpha');
  const planned = await page.request.post(`/api/v1/clusters/${alpha}/operations`, {
    headers,
    data: { type: 'rebalance', params: {} },
  });
  expect(planned.ok()).toBe(true);
  const { id } = (await planned.json()) as { id: string };
  try {
    await page.goto(`/operations/${id}`);
    await capture(page, info, 'rebalance-plan');
  } finally {
    await page.request.delete(`/api/v1/operations/${id}`, { headers });
  }
});
