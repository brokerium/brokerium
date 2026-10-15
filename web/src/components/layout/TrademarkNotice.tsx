// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { Text, type TextProps } from '@mantine/core';

export function TrademarkNotice(props: TextProps) {
  return (
    <Text size="xs" c="dimmed" ta="center" {...props}>
      Apache, Apache Kafka and Kafka are trademarks of the Apache Software Foundation. Brokerium is not affiliated with
      the ASF.
    </Text>
  );
}
