// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { useId } from 'react';

/** The Brokerium mark (docs/brand): three balanced brokers in a ring and a partition being placed. */
export function BrokeriumMark({ size = 32 }: { size?: number }) {
  // Gradient ids must be unique per instance and valid inside url(#...).
  const gradient = `brokerium-${useId().replace(/[^a-zA-Z0-9_-]/g, '')}`;
  const fill = `url(#${gradient})`;
  return (
    <svg width={size} height={size} viewBox="0 0 64 64" role="img" aria-label="Brokerium">
      <defs>
        <linearGradient id={gradient} x1="0" y1="0" x2="1" y2="1">
          <stop offset="0" stopColor="#4C6EF5" />
          <stop offset="1" stopColor="#7950F2" />
        </linearGradient>
      </defs>
      <circle cx="32" cy="32" r="27" fill="none" stroke={fill} strokeWidth="4.5" />
      <rect x="18" y="27.5" width="7" height="20" rx="3.5" fill={fill} />
      <rect x="28.5" y="27.5" width="7" height="20" rx="3.5" fill={fill} />
      <rect x="39" y="27.5" width="7" height="20" rx="3.5" fill={fill} />
      <rect x="28.5" y="16.5" width="7" height="7" rx="2" fill="#91A7FF" />
    </svg>
  );
}
