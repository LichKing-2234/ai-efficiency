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

export function formatCredits(value: number | null | undefined): string {
  if (value == null || !Number.isFinite(value)) return '--'
  return value.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 })
}

// formatUsagePair renders the Token/Credits pair used outside tables, e.g.
// "3.49B/0.06". The two units are always shown side by side and never summed.
export function formatUsagePair(tokens: number | null | undefined, credits: number | null | undefined): string {
  const tokenText = formatTokenCount(tokens)
  return `${tokenText === '-' ? '--' : tokenText}/${formatCredits(credits)}`
}
