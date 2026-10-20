// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

import { useMutation, useQueryClient } from '@tanstack/react-query';

import { api } from '../../api/client';
import { clusterKeys, clusterPath, refreshClusterView } from '../../api/clusters';

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
    mutationFn: (req: DeleteRecordsRequest) =>
      api.post<DeleteRecordsResult>(clusterPath(clusterId, `/topics/${encodeURIComponent(topic)}/delete-records`), req),
    onSuccess: (res) => {
      if (res.dryRun) return;
      void qc.invalidateQueries({ queryKey: clusterKeys.topic(clusterId, topic) });
      void qc.invalidateQueries({ queryKey: ['messages', clusterId, topic] });
      void refreshClusterView(qc, clusterId);
    },
    meta: { errorTitle: 'Could not delete records' },
  });
}
