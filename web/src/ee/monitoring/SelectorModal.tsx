// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

import { Button, Code, Group, Modal, NumberInput, Stack, Text, Textarea } from '@mantine/core';
import { type FormEvent, useState } from 'react';

import { errorMessage } from '../../api/errors';
import { notifySuccess } from '../../components/notify';
import { type MonitoringSelector, useUpdateSelector } from '../api/monitoring';

interface SelectorModalProps {
  clusterId: string;
  clusterName: string;
  selector: MonitoringSelector;
  onClose: () => void;
}

/** Lets administrators replace the selector of a cluster or the port of its exporters. */
export function SelectorModal({ clusterId, clusterName, selector, onClose }: SelectorModalProps) {
  const update = useUpdateSelector(clusterId);
  const [custom, setCustom] = useState(selector.custom);
  const [port, setPort] = useState<number | string>(selector.exporterPort);

  const submit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    update.mutate(
      { selector: custom.trim(), exporterPort: typeof port === 'number' ? port : 0 },
      {
        onSuccess: () => {
          notifySuccess('Selector saved', clusterName);
          onClose();
        },
      },
    );
  };

  return (
    <Modal opened onClose={onClose} title={<Text fw={600}>Metrics of {clusterName} in Prometheus</Text>} size="lg" centered>
      <form onSubmit={submit}>
        <Stack gap="md">
          <Text size="sm">
            The panels select the metrics of this cluster with PromQL label matchers. By default they match the exporters
            on the hosts of its brokers and controllers by their <Code>instance</Code> label, host and exporter port.
          </Text>
          <NumberInput
            label="Exporter port"
            description="Port of the JMX exporter on the brokers, for the default selector"
            min={1}
            max={65535}
            allowDecimal={false}
            value={port}
            onChange={setPort}
            w={220}
          />
          <Textarea
            label="Custom selector"
            description='Label matchers that replace the default, for example job="kafka-eu" or cluster="prod", env=~"eu-.*". Leave empty for the default.'
            placeholder='job="kafka"'
            autosize
            minRows={2}
            maxRows={6}
            value={custom}
            onChange={(e) => {
              setCustom(e.currentTarget.value);
              if (update.error) update.reset();
            }}
            error={update.error ? errorMessage(update.error) : undefined}
            autoComplete="off"
            spellCheck={false}
            styles={{ input: { fontFamily: 'var(--mantine-font-family-monospace)' } }}
          />
          <Stack gap={4}>
            <Text size="xs" c="dimmed">
              Default selector
            </Text>
            <Code block>{selector.default || 'none yet: Brokerium does not know the addresses of the brokers'}</Code>
          </Stack>
          <Group justify="flex-end">
            <Button variant="default" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" loading={update.isPending}>
              Save
            </Button>
          </Group>
        </Stack>
      </form>
    </Modal>
  );
}
