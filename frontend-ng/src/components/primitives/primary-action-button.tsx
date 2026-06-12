import type * as React from 'react'
import { Button } from '@/components/ui/button'

export function PrimaryActionButton(props: Omit<React.ComponentProps<typeof Button>, 'variant'>) {
  return <Button {...props} />
}
