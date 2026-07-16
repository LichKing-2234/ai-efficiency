export interface BrowserTokenPair {
  accessToken: string
  refreshToken: string | null
}

export interface BrowserSessionSnapshot {
  generation: number
  accessToken: string | null
  refreshToken: string | null
}

export type BrowserSessionTransitionKind = 'replace' | 'rotate' | 'clear' | 'expire'

export interface BrowserSessionTransition {
  kind: BrowserSessionTransitionKind
  previous: BrowserSessionSnapshot
  current: BrowserSessionSnapshot
}

export interface AuthExpiryEvent {
  expiredGeneration: number
  clearedGeneration: number
}

const TOKEN_KEY = 'token'
const REFRESH_TOKEN_KEY = 'refresh_token'

let generation = 0
let latestAuthExpiry: AuthExpiryEvent | null = null
const transitionListeners = new Set<(event: BrowserSessionTransition) => void>()
const expiryListeners = new Set<(event: AuthExpiryEvent) => void>()

function copySnapshot(snapshot: BrowserSessionSnapshot): BrowserSessionSnapshot {
  return { ...snapshot }
}

function persist(snapshot: BrowserSessionSnapshot) {
  if (snapshot.accessToken) {
    localStorage.setItem(TOKEN_KEY, snapshot.accessToken)
  } else {
    localStorage.removeItem(TOKEN_KEY)
  }

  if (snapshot.refreshToken) {
    localStorage.setItem(REFRESH_TOKEN_KEY, snapshot.refreshToken)
  } else {
    localStorage.removeItem(REFRESH_TOKEN_KEY)
  }
}

function transition(
  kind: BrowserSessionTransitionKind,
  previous: BrowserSessionSnapshot,
  current: BrowserSessionSnapshot,
) {
  persist(current)
  for (const listener of transitionListeners) {
    listener({
      kind,
      previous: copySnapshot(previous),
      current: copySnapshot(current),
    })
  }
}

export function readBrowserSession(): BrowserSessionSnapshot {
  return {
    generation,
    accessToken: localStorage.getItem(TOKEN_KEY),
    refreshToken: localStorage.getItem(REFRESH_TOKEN_KEY),
  }
}

export function replaceBrowserSession(tokens: BrowserTokenPair): BrowserSessionSnapshot {
  const previous = readBrowserSession()
  generation += 1
  const current: BrowserSessionSnapshot = {
    generation,
    accessToken: tokens.accessToken,
    refreshToken: tokens.refreshToken,
  }
  transition('replace', previous, current)
  return copySnapshot(current)
}

export function rotateBrowserSession(
  expectedGeneration: number,
  tokens: BrowserTokenPair,
): BrowserSessionSnapshot | null {
  const previous = readBrowserSession()
  if (previous.generation !== expectedGeneration) {
    return null
  }

  const current: BrowserSessionSnapshot = {
    generation: previous.generation,
    accessToken: tokens.accessToken,
    refreshToken: tokens.refreshToken,
  }
  transition('rotate', previous, current)
  return copySnapshot(current)
}

export function clearBrowserSession(): BrowserSessionSnapshot {
  const previous = readBrowserSession()
  generation += 1
  const current: BrowserSessionSnapshot = {
    generation,
    accessToken: null,
    refreshToken: null,
  }
  transition('clear', previous, current)
  return copySnapshot(current)
}

export function expireBrowserSession(expectedGeneration: number): AuthExpiryEvent | null {
  const previous = readBrowserSession()
  if (previous.generation !== expectedGeneration) {
    return null
  }

  generation += 1
  const current: BrowserSessionSnapshot = {
    generation,
    accessToken: null,
    refreshToken: null,
  }
  transition('expire', previous, current)

  const event: AuthExpiryEvent = {
    expiredGeneration: previous.generation,
    clearedGeneration: current.generation,
  }
  latestAuthExpiry = { ...event }
  for (const listener of expiryListeners) {
    listener({ ...event })
  }
  return { ...event }
}

export function readLatestAuthExpiry(): AuthExpiryEvent | null {
  return latestAuthExpiry ? { ...latestAuthExpiry } : null
}

export function onBrowserSessionTransition(
  listener: (event: BrowserSessionTransition) => void,
): () => void {
  transitionListeners.add(listener)
  return () => transitionListeners.delete(listener)
}

export function onAuthExpiry(listener: (event: AuthExpiryEvent) => void): () => void {
  expiryListeners.add(listener)
  return () => expiryListeners.delete(listener)
}
