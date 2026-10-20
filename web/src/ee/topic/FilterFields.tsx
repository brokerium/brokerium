// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

import { Checkbox, Group, Select, SimpleGrid, TextInput } from '@mantine/core';

import type { FilterState, MatchMode } from './filter';

const MODES = [
  { value: 'contains', label: 'Contains' },
  { value: 'regex', label: 'Regex' },
];

interface FilterFieldsProps {
  value: FilterState;
  onChange: (next: FilterState) => void;
  partitionCount: number;
}

export function FilterFields({ value: f, onChange, partitionCount }: FilterFieldsProps) {
  const set = (patch: Partial<FilterState>) => onChange({ ...f, ...patch });
  const modeSelect = (mode: MatchMode, field: 'keyMode' | 'valueMode', label: string) => (
    <Select
      aria-label={label}
      w={110}
      data={MODES}
      value={mode}
      allowDeselect={false}
      onChange={(v) => v && set(field === 'keyMode' ? { keyMode: v as MatchMode } : { valueMode: v as MatchMode })}
    />
  );
  return (
    <SimpleGrid cols={{ base: 1, md: 2 }} spacing="sm">
      <Group gap="xs" align="flex-end" wrap="nowrap">
        {modeSelect(f.keyMode, 'keyMode', 'Key match')}
        <TextInput
          style={{ flex: 1 }}
          label="Key"
          placeholder="Any"
          value={f.key}
          onChange={(e) => set({ key: e.currentTarget.value })}
        />
      </Group>
      <Group gap="xs" align="flex-end" wrap="nowrap">
        {modeSelect(f.valueMode, 'valueMode', 'Value match')}
        <TextInput
          style={{ flex: 1 }}
          label="Value"
          placeholder="Any"
          value={f.value}
          onChange={(e) => set({ value: e.currentTarget.value })}
        />
      </Group>
      <Group gap="xs" grow>
        <TextInput
          label="Header"
          placeholder="Any"
          value={f.headerKey}
          onChange={(e) => set({ headerKey: e.currentTarget.value })}
        />
        <TextInput
          label="Header value contains"
          placeholder="Any"
          disabled={!f.headerKey}
          value={f.headerValue}
          onChange={(e) => set({ headerValue: e.currentTarget.value })}
        />
      </Group>
      <Group gap="xs" grow>
        <TextInput
          label="From (local time)"
          type="datetime-local"
          step={1}
          value={f.from}
          onChange={(e) => set({ from: e.currentTarget.value })}
        />
        <TextInput
          label="Until (local time)"
          type="datetime-local"
          step={1}
          value={f.to}
          onChange={(e) => set({ to: e.currentTarget.value })}
        />
      </Group>
      <Group gap="md" align="flex-end">
        <Select
          label="Partition"
          w={140}
          data={[{ value: 'all', label: 'All' }, ...Array.from({ length: partitionCount }, (_, i) => String(i))]}
          value={f.partition}
          allowDeselect={false}
          onChange={(v) => set({ partition: v ?? 'all' })}
        />
        <Checkbox
          mb={8}
          label="Ignore case"
          checked={f.ignoreCase}
          onChange={(e) => set({ ignoreCase: e.currentTarget.checked })}
        />
      </Group>
    </SimpleGrid>
  );
}
