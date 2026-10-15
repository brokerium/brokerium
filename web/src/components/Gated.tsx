// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { Button, type ButtonProps, Menu, type MenuItemProps, Tooltip } from '@mantine/core';
import type { MouseEvent, ReactNode } from 'react';

import type { Availability } from '../lib/capabilities';

interface GatedButtonProps extends ButtonProps {
  availability: Availability;
  onClick?: () => void;
  type?: 'button' | 'submit';
  children: ReactNode;
}

/**
 * Button that stays visible but disabled, with the reason in a tooltip, when not allowed.
 * An allowed action may still carry a reason, shown as a hint.
 */
export function GatedButton({ availability, onClick, children, ...props }: GatedButtonProps) {
  const { allowed, reason } = availability;
  const handleClick = (event: MouseEvent<HTMLButtonElement>) => {
    if (!allowed) {
      event.preventDefault();
      return;
    }
    onClick?.();
  };
  return (
    <Tooltip label={reason} disabled={!reason} multiline maw={320} withArrow>
      <Button {...props} data-disabled={!allowed || undefined} aria-disabled={!allowed} onClick={handleClick}>
        {children}
      </Button>
    </Tooltip>
  );
}

interface GatedMenuItemProps extends MenuItemProps {
  availability: Availability;
  onClick?: () => void;
  children: ReactNode;
}

export function GatedMenuItem({ availability, onClick, children, ...props }: GatedMenuItemProps) {
  return (
    <Menu.Item {...props} disabled={!availability.allowed} title={availability.reason} onClick={onClick}>
      {children}
    </Menu.Item>
  );
}
