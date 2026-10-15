// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { Badge, Group, Table, Text } from '@mantine/core';

import type { Feature } from '../api/types';

/** Kafka features (metadata.version, kraft.version, ...) with finalized and supported levels. */
export function FeaturesTable({ features }: { features: Feature[] }) {
  if (features.length === 0) {
    return (
      <Text size="sm" c="dimmed">
        No feature information.
      </Text>
    );
  }
  return (
    <Table.ScrollContainer minWidth={480}>
      <Table striped verticalSpacing={6}>
        <Table.Thead>
          <Table.Tr>
            <Table.Th>Feature</Table.Th>
            <Table.Th>Finalized level</Table.Th>
            <Table.Th>Supported range</Table.Th>
          </Table.Tr>
        </Table.Thead>
        <Table.Tbody>
          {[...features]
            .sort((a, b) => a.name.localeCompare(b.name))
            .map((f) => (
              <Table.Tr key={f.name}>
                <Table.Td>
                  <Text size="sm" ff="monospace">
                    {f.name}
                  </Text>
                </Table.Td>
                <Table.Td>
                  <Group gap={6}>
                    <Text size="sm" fw={600}>
                      {f.finalized}
                    </Text>
                    {f.finalized < f.supportedMax && (
                      <Badge size="xs" variant="light" color="blue">
                        up to {f.supportedMax} supported
                      </Badge>
                    )}
                  </Group>
                </Table.Td>
                <Table.Td>
                  <Text size="sm">
                    {f.supportedMin} – {f.supportedMax}
                  </Text>
                </Table.Td>
              </Table.Tr>
            ))}
        </Table.Tbody>
      </Table>
    </Table.ScrollContainer>
  );
}
