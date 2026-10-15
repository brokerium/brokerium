// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { useCallback, useState } from 'react';

export interface FormState<T> {
  values: T;
  set: <K extends keyof T>(key: K, value: T[K]) => void;
  setValues: (values: T) => void;
  reset: () => void;
}

/** Minimal form state for small dialogs. */
export function useFormState<T>(initial: T): FormState<T> {
  const [values, setValues] = useState<T>(initial);
  const set = useCallback(<K extends keyof T>(key: K, value: T[K]) => {
    setValues((prev) => ({ ...prev, [key]: value }));
  }, []);
  const reset = useCallback(() => setValues(initial), [initial]);
  return { values, set, setValues, reset };
}
