// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { useSyncExternalStore } from 'react';

const listeners = new Set<() => void>();
let now = Date.now();
let timer: ReturnType<typeof setInterval> | undefined;

function subscribe(listener: () => void) {
  listeners.add(listener);
  if (!timer) {
    timer = setInterval(() => {
      now = Date.now();
      for (const l of listeners) l();
    }, 1000);
  }
  return () => {
    listeners.delete(listener);
    if (listeners.size === 0 && timer) {
      clearInterval(timer);
      timer = undefined;
    }
  };
}

function snapshot() {
  return now;
}

/** Current time that updates every second, shared by all relative timestamps on the page. */
export function useNow(): number {
  return useSyncExternalStore(subscribe, snapshot, snapshot);
}
