// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { api } from '../../api/client';
import { clusterPath } from '../../api/clusters';

export type MonitoringSection = 'cluster' | 'broker' | 'topic';

export type MonitoringUnit =
  | 'bytes'
  | 'bytesPerSecond'
  | 'messagesPerSecond'
  | 'requestsPerSecond'
  | 'perMinute'
  | 'milliseconds'
  | 'percent'
  | 'cores'
  | 'count';

export type MonitoringRange = '1h' | '6h' | '24h' | '7d';

export type SourceAuth = 'none' | 'basic' | 'bearer';

export type SourceOrigin = 'none' | 'environment' | 'database';

/** A chart of the Monitoring tab; the server runs its queries. */
export interface MonitoringPanel {
  id: string;
  title: string;
  section: MonitoringSection;
  unit: MonitoringUnit;
  description?: string;
}

/** What the JMX exporters of a cluster export. */
export interface MonitoringProfile {
  /** Generation of the exporter; empty when no JVM metric matches the selector. */
  exporter: '' | '0.x' | '1.x';
  heapMetric: string;
  kraft: boolean;
  /** Brokers that export replica manager metrics. */
  brokers: number;
  detectedAt: string;
  error?: string;
}

export interface MonitoringSelector {
  /** The selector the panels use: custom, else default. */
  effective: string;
  custom: string;
  default: string;
  exporterPort: number;
  updatedAt?: string;
  updatedBy?: string;
}

/** GET /clusters/{id}/monitoring */
export interface ClusterMonitoring {
  source: { configured: boolean; origin: SourceOrigin; error?: string };
  selector: MonitoringSelector;
  profile: MonitoringProfile | null;
  panels: MonitoringPanel[];
  ranges: MonitoringRange[];
}

export interface PanelSeries {
  name: string;
  labels: Record<string, string>;
  /** Pairs of Unix seconds and a value. */
  points: [number, number][];
}

/** GET /clusters/{id}/monitoring/panels/{panel} */
export interface PanelData {
  panel: string;
  unit: MonitoringUnit;
  start: number;
  end: number;
  step: number;
  series: PanelSeries[];
  truncated: boolean;
  warnings?: string[];
}

/** The Prometheus data source without its credentials. */
export interface PrometheusSource {
  configured: boolean;
  origin: SourceOrigin;
  url: string;
  auth: SourceAuth;
  username: string;
  passwordSet: boolean;
  tokenSet: boolean;
  caPem: string;
  insecureSkipVerify: boolean;
  timeoutSeconds: number;
  updatedAt?: string;
  updatedBy?: string;
  error?: string;
}

/** GET /monitoring */
export interface MonitoringSettings {
  source: PrometheusSource;
  metricsEndpoint: { enabled: boolean; tokenSource?: 'environment' | 'file'; error?: string };
}

/** Password and token are write-only: leaving them out keeps the stored ones. */
export interface SourceRequest {
  url: string;
  auth: SourceAuth;
  username?: string;
  password?: string;
  token?: string;
  caPem?: string;
  insecureSkipVerify?: boolean;
  timeoutSeconds?: number;
}

export interface SourceTestResult {
  ok: boolean;
  version?: string;
  durationMs: number;
  error?: string;
}

/** Empty values restore the defaults. */
export interface SelectorRequest {
  selector: string;
  exporterPort: number;
}

export const monitoringKeys = {
  all: ['monitoring'] as const,
  settings: ['monitoring', 'settings'] as const,
  clusters: ['monitoring', 'clusters'] as const,
  cluster: (id: string) => ['monitoring', 'clusters', id] as const,
  panel: (id: string, panel: string, range: MonitoringRange) => ['monitoring', 'clusters', id, 'panels', panel, range] as const,
};

const REFRESH: Record<MonitoringRange, number> = { '1h': 30_000, '6h': 60_000, '24h': 300_000, '7d': 600_000 };

export function useClusterMonitoring(clusterId: string, enabled: boolean) {
  return useQuery({
    queryKey: monitoringKeys.cluster(clusterId),
    queryFn: () => api.get<ClusterMonitoring>(clusterPath(clusterId, '/monitoring')),
    enabled,
    refetchInterval: 60_000,
  });
}

export function usePanelData(clusterId: string, panel: string, range: MonitoringRange, enabled: boolean) {
  return useQuery({
    queryKey: monitoringKeys.panel(clusterId, panel, range),
    queryFn: () =>
      api.get<PanelData>(clusterPath(clusterId, `/monitoring/panels/${encodeURIComponent(panel)}?range=${range}`)),
    enabled,
    refetchInterval: REFRESH[range],
    retry: false,
    meta: { silent: true },
  });
}

export function useUpdateSelector(clusterId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (req: SelectorRequest) => api.put<ClusterMonitoring>(clusterPath(clusterId, '/monitoring'), req),
    onSuccess: (monitoring) => {
      qc.setQueryData(monitoringKeys.cluster(clusterId), monitoring);
      void qc.invalidateQueries({ queryKey: [...monitoringKeys.cluster(clusterId), 'panels'] });
    },
    meta: { silent: true },
  });
}

export function useMonitoringSettings(enabled: boolean) {
  return useQuery({
    queryKey: monitoringKeys.settings,
    queryFn: () => api.get<MonitoringSettings>('/monitoring'),
    enabled,
  });
}

export function useSaveSource() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (req: SourceRequest) => api.put<MonitoringSettings>('/monitoring/source', req),
    onSuccess: (settings) => {
      qc.setQueryData(monitoringKeys.settings, settings);
      void qc.invalidateQueries({ queryKey: monitoringKeys.clusters });
    },
    meta: { silent: true },
  });
}

export function useDeleteSource() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.del<void>('/monitoring/source'),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: monitoringKeys.all });
    },
    meta: { errorTitle: 'Could not remove the data source' },
  });
}

/** Tests the settings of the form, or the source in use without a URL. */
export function useTestSource() {
  return useMutation({
    mutationFn: (req: Partial<SourceRequest>) => api.post<SourceTestResult>('/monitoring/source/test', req),
    meta: { errorTitle: 'Could not test the data source' },
  });
}
