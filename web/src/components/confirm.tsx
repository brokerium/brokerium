// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { Text } from '@mantine/core';
import { modals } from '@mantine/modals';

import { ConfirmBody, type ConfirmOptions } from './ConfirmBody';

export type { ConfirmOptions } from './ConfirmBody';

let counter = 0;

/** Opens a confirmation dialog; resolves to true when the user confirms. */
export function confirmAction(options: ConfirmOptions): Promise<boolean> {
  counter += 1;
  const modalId = `confirm-${counter}`;
  return new Promise((resolve) => {
    let settled = false;
    const finish = (value: boolean) => {
      if (settled) return;
      settled = true;
      resolve(value);
    };
    modals.open({
      modalId,
      title: <Text fw={600}>{options.title}</Text>,
      centered: true,
      onClose: () => finish(false),
      children: (
        <ConfirmBody
          options={options}
          onConfirm={() => {
            finish(true);
            modals.close(modalId);
          }}
          onCancel={() => modals.close(modalId)}
        />
      ),
    });
  });
}
