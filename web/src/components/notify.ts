// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { notifications } from '@mantine/notifications';

export function notifySuccess(title: string, message?: string) {
  notifications.show({ color: 'green', title, message: message ?? '', autoClose: 4000 });
}

export function notifyInfo(title: string, message?: string) {
  notifications.show({ color: 'blue', title, message: message ?? '', autoClose: 5000 });
}
