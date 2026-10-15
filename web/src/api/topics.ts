// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { queryString } from '../lib/format';
import { api } from './client';
import { type ConfigPatch, clusterKeys, clusterPath, refreshClusterView } from './clusters';
import type { Message, MessageHeader, TopicDetail } from './types';

export interface CreateTopicRequest {
  name: string;
  partitions: number;
  replicationFactor: number;
  configs?: Record<string, string>;
}

export interface AddPartitionsRequest {
  /** New total number of partitions. */
  count: number;
}

export type MessageFrom = 'earliest' | 'latest' | 'offset' | 'timestamp';

export interface MessageQuery {
  /** -1 reads all partitions. */
  partition: number;
  from: MessageFrom;
  offset?: number;
  /** Epoch milliseconds. */
  timestamp?: number;
  limit: number;
}

export interface MessagesResponse {
  messages: Message[];
}

export interface ProduceRequest {
  partition?: number;
  key?: string;
  value: string;
  headers?: MessageHeader[];
}

export interface ProduceResponse {
  partition: number;
  offset: number;
}

const enc = encodeURIComponent;

function topicPath(id: string, topic: string, suffix = ''): string {
  return clusterPath(id, `/topics/${enc(topic)}${suffix}`);
}

export const messageKeys = {
  list: (id: string, topic: string, query: MessageQuery | null) => ['messages', id, topic, query] as const,
};

export function useTopicDetail(id: string, topic: string) {
  return useQuery({
    queryKey: clusterKeys.topic(id, topic),
    queryFn: () => api.get<TopicDetail>(topicPath(id, topic)),
    refetchInterval: 10_000,
  });
}

export function useCreateTopic(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (req: CreateTopicRequest) => api.post<unknown>(clusterPath(id, '/topics'), req),
    onSuccess: () => refreshClusterView(qc, id),
    meta: { errorTitle: 'Could not create the topic' },
  });
}

export function useDeleteTopic(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (topic: string) => api.del<void>(topicPath(id, topic, queryString({ confirm: topic }))),
    onSuccess: (_data, topic) => {
      qc.removeQueries({ queryKey: clusterKeys.topic(id, topic) });
      return refreshClusterView(qc, id);
    },
    meta: { errorTitle: 'Could not delete the topic' },
  });
}

export function useAlterTopicConfigs(id: string, topic: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (patch: ConfigPatch) => api.patch<unknown>(topicPath(id, topic, '/configs'), patch),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: clusterKeys.topic(id, topic) });
      void refreshClusterView(qc, id);
    },
    meta: { errorTitle: 'Could not change topic configs' },
  });
}

export function useAddPartitions(id: string, topic: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (req: AddPartitionsRequest) => api.post<unknown>(topicPath(id, topic, '/partitions'), req),
    onSuccess: async () => {
      await refreshClusterView(qc, id);
      await qc.invalidateQueries({ queryKey: clusterKeys.topic(id, topic) });
    },
    meta: { errorTitle: 'Could not add partitions' },
  });
}

export function useMessages(id: string, topic: string, query: MessageQuery | null) {
  return useQuery({
    queryKey: messageKeys.list(id, topic, query),
    queryFn: async () => {
      const q = query as MessageQuery;
      const qs = queryString({
        partition: q.partition,
        from: q.from,
        offset: q.from === 'offset' ? q.offset : undefined,
        timestamp: q.from === 'timestamp' ? q.timestamp : undefined,
        limit: q.limit,
      });
      return (await api.get<MessagesResponse>(topicPath(id, topic, `/messages${qs}`))).messages;
    },
    enabled: query !== null,
    staleTime: Infinity,
    meta: { silent: true },
  });
}

export function useProduceMessage(id: string, topic: string) {
  return useMutation({
    mutationFn: (req: ProduceRequest) => api.post<ProduceResponse>(topicPath(id, topic, '/messages'), req),
    meta: { errorTitle: 'Could not produce the record' },
  });
}
