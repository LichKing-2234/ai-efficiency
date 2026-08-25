export const FULL_PAGE_SIZES = [20, 50, 100]

export function positivePage(value: unknown, fallback = 1) {
  const parsed = Number(Array.isArray(value) ? value[0] : value)
  return Number.isInteger(parsed) && parsed > 0 ? parsed : fallback
}

export function fullPageSize(value: unknown) {
  const parsed = positivePage(value, 20)
  return FULL_PAGE_SIZES.includes(parsed) ? parsed : 20
}
