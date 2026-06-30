export function formatCompactNumber(value: number | null | undefined): string {
  if (value == null || !Number.isFinite(value)) return '-'
  const absoluteValue = Math.abs(value)
  if (absoluteValue >= 1_000_000_000) return `${trimFixed(value / 1_000_000_000)}B`
  if (absoluteValue >= 1_000_000) return `${trimFixed(value / 1_000_000)}M`
  if (absoluteValue >= 1_000) return `${trimFixed(value / 1_000)}K`
  return Math.round(value).toLocaleString()
}

export function formatTokenCount(value: number | null | undefined): string {
  return formatCompactNumber(value)
}

function trimFixed(value: number): string {
  return value.toFixed(2).replace(/\.?0+$/, '')
}
