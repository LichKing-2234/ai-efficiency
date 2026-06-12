import type * as React from 'react'
import { Button } from '@/components/ui/button'

export function QuietActionButton(props: React.ComponentProps<typeof Button>) {
  return <Button size='sm' variant='ghost' {...props} />
}
