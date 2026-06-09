import { describe, expect, test } from 'vitest'
import { buildSparklinePath, buildStackedAreaLayers, type StackedAreaKey, type StackedAreaPoint } from './charts'

describe('buildSparklinePath', () => {
  test('builds a stable sparkline for flat data without NaN coordinates', () => {
    const path = buildSparklinePath([5, 5, 5], 90, 30)

    expect(path.line).toBe('M0.0 27.0 L45.0 27.0 L90.0 27.0')
    expect(path.area).not.toContain('NaN')
    expect(path.last).toEqual([90, 27])
  })
})

describe('buildStackedAreaLayers', () => {
  test('stacks token series in key order and returns one layer per key', () => {
    type Point = StackedAreaPoint & { date: string; input: number; output: number }
    const keys: Array<StackedAreaKey<Point>> = [
      { key: 'input', label: 'Input', color: 'var(--viz-input)' },
      { key: 'output', label: 'Output', color: 'var(--viz-output)' }
    ]
    const result = buildStackedAreaLayers({
      series: [
        { date: '2026-06-01', input: 10, output: 5 },
        { date: '2026-06-02', input: 20, output: 10 }
      ],
      keys,
      width: 120,
      height: 80,
      pad: { left: 10, right: 10, top: 10, bottom: 10 }
    })

    expect(result.layers).toHaveLength(2)
    expect(result.layers.map((layer) => layer.key)).toEqual(['input', 'output'])
    expect(result.totals).toEqual([15, 30])
    expect(result.layers[0].area).toContain('Z')
    expect(result.layers[1].path).toContain('L110.0')
  })
})
