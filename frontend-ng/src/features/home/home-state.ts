export type HomeSetupInput = {
  connectedTools: number
  totalRepos?: number | null
  recentEvents: number
}

export function buildHomeSetupItems(input: HomeSetupInput) {
  return [
    { id: 'account', ready: true },
    { id: 'ai-access', ready: input.connectedTools > 0 },
    { id: 'repos', ready: (input.totalRepos ?? 0) > 0 },
    { id: 'usage', ready: input.recentEvents > 0 }
  ]
}

export function homeSetupProgress(input: HomeSetupInput) {
  const items = buildHomeSetupItems(input)
  const ready = items.filter((item) => item.ready).length
  return {
    ready,
    total: items.length,
    ratio: items.length ? ready / items.length : 0
  }
}
