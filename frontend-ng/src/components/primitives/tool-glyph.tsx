import { cn } from '@/lib/utils'

const TOOL_META: Record<string, { glyph: string; color: string }> = {
  claude: { glyph: 'C', color: 'var(--viz-input)' },
  codex: { glyph: 'X', color: 'var(--viz-output)' },
  kiro: { glyph: 'K', color: 'var(--viz-reason)' }
}

export function ToolGlyph({
  tool,
  label,
  size = 24,
  className
}: {
  tool?: string | null
  label?: string
  size?: number
  className?: string
}) {
  const key = (tool ?? '').toLowerCase()
  const meta = TOOL_META[key] ?? {
    glyph: (label ?? tool ?? '?').slice(0, 1).toUpperCase(),
    color: 'var(--ink-3)'
  }

  return (
    <span
      aria-label={label ?? tool ?? undefined}
      className={cn('grid shrink-0 place-items-center border font-mono font-bold', className)}
      style={{
        width: size,
        height: size,
        borderRadius: Math.max(6, Math.round(size * 0.29)),
        background: `color-mix(in oklab, ${meta.color} 14%, transparent)`,
        borderColor: `color-mix(in oklab, ${meta.color} 32%, transparent)`,
        color: meta.color,
        fontSize: Math.round(size * 0.46)
      }}
      title={label ?? tool ?? undefined}
    >
      {meta.glyph}
    </span>
  )
}
