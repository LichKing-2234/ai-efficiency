import type * as React from 'react'
import { DataGridCell } from '@/components/primitives/data-grid'
import { ToolGlyph } from '@/components/primitives/tool-glyph'
import { cn } from '@/lib/utils'

export function GlyphLabelCell({
  children,
  className,
  description,
  glyphLabel,
  glyphTool,
  mono = false,
  size = 22,
  truncate = false
}: {
  children: React.ReactNode
  className?: string
  description?: React.ReactNode
  glyphLabel?: string
  glyphTool?: string | null
  mono?: boolean
  size?: number
  truncate?: boolean
}) {
  return (
    <div className={cn('flex min-w-0 items-center gap-2', className)} data-slot='glyph-label-cell'>
      <ToolGlyph label={glyphLabel} tool={glyphTool} size={size} />
      <DataGridCell description={description} mono={mono} truncate={truncate}>{children}</DataGridCell>
    </div>
  )
}
