// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { MultiSelect, Select, Stack } from '@mantine/core';
import { useState } from 'react';

import { useNodes } from '../../api/nodes';
import { hostLabel, isFreeHost } from '../../lib/bootstrap';
import { AddHost } from './JoinCommand';

/** Connected agent hosts that can join the cluster, polled faster while a new host is added. */
function useFreeHosts(clusterId: string, waiting: boolean) {
  const { data: nodes, isPending } = useNodes(undefined, waiting ? 3_000 : 10_000);
  const free = (nodes ?? []).filter((n) => n.connected && isFreeHost(n, clusterId)).sort((a, b) => a.name.localeCompare(b.name));
  return { options: free.map((n) => ({ value: n.id, label: hostLabel(n) })), isPending };
}

interface HostSelectProps {
  clusterId: string;
  label: string;
  description?: string;
  placeholder?: string;
  required?: boolean;
  value: string | null;
  onChange: (value: string | null) => void;
}

/** Picks one free agent host for a new node of an agent cluster. */
export function HostSelect({ clusterId, label, description, placeholder, required, value, onChange }: HostSelectProps) {
  const [waiting, setWaiting] = useState(false);
  const { options, isPending } = useFreeHosts(clusterId, waiting);
  return (
    <Stack gap="xs">
      <Select
        label={label}
        description={description}
        placeholder={isPending ? 'Loading hosts…' : (placeholder ?? 'Choose a host')}
        data={options}
        value={value}
        onChange={onChange}
        required={required}
        clearable={!required}
        searchable
        nothingFoundMessage="No connected host without a role"
      />
      <AddHost clusterId={clusterId} onWaitingChange={setWaiting} />
    </Stack>
  );
}

interface HostMultiSelectProps {
  clusterId: string;
  label: string;
  description?: string;
  value: string[];
  onChange: (value: string[]) => void;
}

/** Picks free agent hosts for new nodes of an agent cluster. */
export function HostMultiSelect({ clusterId, label, description, value, onChange }: HostMultiSelectProps) {
  const [waiting, setWaiting] = useState(false);
  const { options, isPending } = useFreeHosts(clusterId, waiting);
  return (
    <Stack gap="xs">
      <MultiSelect
        label={label}
        description={description}
        placeholder={isPending ? 'Loading hosts…' : 'Choose hosts'}
        data={options}
        value={value}
        onChange={onChange}
        required
        searchable
        nothingFoundMessage="No connected host without a role"
      />
      <AddHost clusterId={clusterId} onWaitingChange={setWaiting} />
    </Stack>
  );
}
