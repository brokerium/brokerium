// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

import { useMutation, useQueryClient } from '@tanstack/react-query';

import { api } from '../../api/client';
import { clusterKeys, clusterPath, refreshClusterView } from '../../api/clusters';
import { operationKeys } from '../../api/operations';
import type { Message, Operation } from '../../api/types';

const topicPath = (clusterId: string, topic: string, suffix: string) =>
  clusterPath(clusterId, `/topics/${encodeURIComponent(topic)}${suffix}`);

/** Bytes that contain a text or match a regular expression (RE2 syntax). */
export interface Match {
  contains?: string;
  regex?: string;
}

export interface MessageFilter {
  key?: Match;
  value?: Match;
  header?: { key: string; value?: Match };
  ignoreCase?: boolean;
}

export interface Selection {
  /** All partitions when empty. */
  partitions?: number[];
  /** Unix milliseconds; toTime is exclusive. */
  fromTime?: number;
  toTime?: number;
  /** Continues an earlier search from these offsets. */
  resume?: PartitionOffset[];
}

export interface SearchRequest extends Selection, MessageFilter {
  limit?: number;
  maxRecords?: number;
  timeoutSeconds?: number;
}

export interface SearchResult {
  messages: Message[];
  scanned: number;
  next: PartitionOffset[];
  complete: boolean;
  stopped?: 'limit' | 'records' | 'time';
}

export type ExportFormat = 'ndjson' | 'json' | 'csv';

export interface ExportRequest extends Selection, MessageFilter {
  format: ExportFormat;
  limit?: number;
}

export interface CopyParams extends Selection, MessageFilter {
  topic: string;
  targetCluster?: string;
  targetTopic: string;
  partitioning?: 'same' | 'key';
  keepTimestamps?: boolean;
  throttleBytesPerSec?: number;
}

export function useSearchMessages(clusterId: string, topic: string) {
  return useMutation({
    mutationFn: (req: SearchRequest) => api.post<SearchResult>(topicPath(clusterId, topic, '/search'), req),
    meta: { errorTitle: 'Could not search records' },
  });
}

export function useExportMessages(clusterId: string, topic: string) {
  return useMutation({
    mutationFn: (req: ExportRequest) => api.download(topicPath(clusterId, topic, '/export'), req, `${topic}.${req.format}`),
    meta: { errorTitle: 'Could not export records' },
  });
}

/** Plans a copy-messages operation; it starts once confirmed on the operation page. */
export function usePlanCopy(clusterId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (params: CopyParams) =>
      api.post<Operation>(clusterPath(clusterId, '/operations'), { type: 'copy-messages', params }),
    onSuccess: (op) => {
      qc.setQueryData(operationKeys.detail(op.id), op);
      void qc.invalidateQueries({ queryKey: operationKeys.cluster(clusterId) });
    },
    meta: { errorTitle: 'Could not plan the copy' },
  });
}

export interface PartitionOffset {
  partition: number;
  /** The records before this offset are deleted. */
  offset: number;
}

/** Exactly one of partitions, beforeTimestamp and all is set. */
export interface DeleteRecordsRequest {
  partitions?: PartitionOffset[];
  /** Unix milliseconds. */
  beforeTimestamp?: number;
  all?: boolean;
  dryRun: boolean;
  /** The topic name; required unless dryRun is set. */
  confirm?: string;
}

export interface DeletePartition {
  partition: number;
  startOffset: number;
  endOffset: number;
  /** The new start offset of the partition. */
  deleteBefore: number;
  records: number;
  error?: string;
}

export interface DeleteRecordsResult {
  partitions: DeletePartition[];
  records: number;
  failed: number;
  dryRun: boolean;
}

export function useDeleteRecords(clusterId: string, topic: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (req: DeleteRecordsRequest) => api.post<DeleteRecordsResult>(topicPath(clusterId, topic, '/delete-records'), req),
    onSuccess: (res) => {
      if (res.dryRun) return;
      void qc.invalidateQueries({ queryKey: clusterKeys.topic(clusterId, topic) });
      void qc.invalidateQueries({ queryKey: ['messages', clusterId, topic] });
      void refreshClusterView(qc, clusterId);
    },
    meta: { errorTitle: 'Could not delete records' },
  });
}
