import { useId, useMemo, useRef, useState } from 'react'
import { compact } from '@/lib/format'
import { cn } from '@/lib/utils'
import { ActionGroup } from './action-group'
import { Stack } from './stack'

export type StackedAreaKey<T extends object> = {
  key: keyof T & string
  label: string
  color: string
}

export type StackedAreaPoint = object

function pointValue<T extends object>(point: T, key: keyof T & string) {
  const value = point[key]
  return typeof value === 'number' ? value : Number(value ?? 0)
}

function pointLabel<T extends object>(point: T, key: string) {
  const value = (point as Record<string, unknown>)[key]
  return typeof value === 'string' || typeof value === 'number' ? String(value) : ''
}

function StackedAreaTooltipRow({
  color,
  label,
  value
}: {
  color: string
  label: string
  value: React.ReactNode
}) {
  return (
    <ActionGroup align='start' className='mt-1 text-[11.5px]' dataSlot='stacked-area-tooltip-row' fit>
      <span className='size-2 rounded-[3px]' style={{ background: color }} />
      <span className='flex-1 text-[var(--ink-2)]'>{label}</span>
      <span className='mono tnum font-semibold'>{value}</span>
    </ActionGroup>
  )
}

export function buildSparklinePath(data: number[], width: number, height: number) {
  const values = data.length ? data : [0]
  const min = Math.min(...values)
  const max = Math.max(...values)
  const range = max - min || 1
  const points = values.map((value, index) => {
    const x = values.length === 1 ? width : (index / (values.length - 1)) * width
    const y = height - 3 - ((value - min) / range) * (height - 6)
    return [x, y] as const
  })
  const line = points.map(([x, y], index) => `${index === 0 ? 'M' : 'L'}${x.toFixed(1)} ${y.toFixed(1)}`).join(' ')
  const area = `${line} L${width} ${height} L0 ${height} Z`
  return { line, area, last: points[points.length - 1] ?? ([0, height / 2] as const) }
}

export function buildStackedAreaLayers<T extends object>({
  series,
  keys,
  width,
  height,
  pad
}: {
  series: T[]
  keys: Array<StackedAreaKey<T>>
  width: number
  height: number
  pad: { left: number; right: number; top: number; bottom: number }
}) {
  const values = series.length ? series : [{} as T]
  const innerWidth = Math.max(1, width - pad.left - pad.right)
  const innerHeight = Math.max(1, height - pad.top - pad.bottom)
  const totals = values.map((point) => keys.reduce((sum, key) => sum + pointValue(point, key.key), 0))
  const max = Math.max(1, ...totals) * 1.12
  const x = (index: number) => pad.left + (values.length === 1 ? innerWidth : (index / (values.length - 1)) * innerWidth)
  const y = (value: number) => pad.top + innerHeight - (value / max) * innerHeight
  const base = values.map(() => 0)
  const layers = keys.map((key) => {
    const top = values.map((point, index) => base[index] + pointValue(point, key.key))
    const path = values.map((_, index) => `${index === 0 ? 'M' : 'L'}${x(index).toFixed(1)} ${y(top[index]).toFixed(1)}`).join(' ')
    const floor = values.map((_, index) => `L${x(values.length - 1 - index).toFixed(1)} ${y(base[values.length - 1 - index]).toFixed(1)}`).join(' ')
    const area = `${path} L${x(values.length - 1).toFixed(1)} ${y(base[values.length - 1]).toFixed(1)} ${floor} Z`
    values.forEach((_, index) => {
      base[index] = top[index]
    })
    return { ...key, path, area }
  })
  return { layers, totals, max, x, y }
}

export function Sparkline({
  data,
  width = 92,
  height = 30,
  color = 'var(--ai)',
  fill = true,
  strokeWidth = 1.6,
  className
}: {
  data: number[]
  width?: number
  height?: number
  color?: string
  fill?: boolean
  strokeWidth?: number
  className?: string
}) {
  const gradientId = useId()
  const { line, area, last } = useMemo(() => buildSparklinePath(data, width, height), [data, height, width])
  return (
    <svg aria-hidden='true' className={cn('block overflow-visible', className)} height={height} viewBox={`0 0 ${width} ${height}`} width={width}>
      {fill ? (
        <defs>
          <linearGradient id={gradientId} x1='0' x2='0' y1='0' y2='1'>
            <stop offset='0%' stopColor={color} stopOpacity='.22' />
            <stop offset='100%' stopColor={color} stopOpacity='0' />
          </linearGradient>
        </defs>
      ) : null}
      {fill ? <path d={area} fill={`url(#${gradientId})`} /> : null}
      <path d={line} fill='none' stroke={color} strokeLinecap='round' strokeLinejoin='round' strokeWidth={strokeWidth} />
      <circle cx={last[0]} cy={last[1]} fill={color} r='2.2' />
    </svg>
  )
}

export function SparkBars({
  data,
  width = 120,
  height = 30,
  color = 'var(--ai)',
  gap = 2,
  className
}: {
  data: Array<number | { value: number }>
  width?: number
  height?: number
  color?: string
  gap?: number
  className?: string
}) {
  const values = data.map((item) => typeof item === 'number' ? item : item.value)
  const max = Math.max(1, ...values)
  const barWidth = values.length ? (width - gap * (values.length - 1)) / values.length : width
  return (
    <svg aria-hidden='true' className={cn('block', className)} height={height} viewBox={`0 0 ${width} ${height}`} width={width}>
      {values.map((value, index) => {
        const barHeight = Math.max(2, (value / max) * height)
        return (
          <rect
            fill={color}
            height={barHeight}
            key={`${value}-${index}`}
            opacity={0.55 + 0.45 * (value / max)}
            rx={Math.min(2, barWidth / 2)}
            width={barWidth}
            x={index * (barWidth + gap)}
            y={height - barHeight}
          />
        )
      })}
    </svg>
  )
}

export function Ring({
  value,
  size = 88,
  stroke = 8,
  color = 'var(--ai)',
  track = 'var(--surface-3)',
  children,
  className
}: {
  value: number
  size?: number
  stroke?: number
  color?: string
  track?: string
  children?: React.ReactNode
  className?: string
}) {
  const normalized = Math.max(0, Math.min(1, value))
  const radius = (size - stroke) / 2
  const circumference = 2 * Math.PI * radius
  return (
    <Stack className={cn('relative items-center justify-center', className)} dataSlot='ring' gap='none' style={{ width: size, height: size }}>
      <svg aria-hidden='true' height={size} style={{ transform: 'rotate(-90deg)' }} width={size}>
        <circle cx={size / 2} cy={size / 2} fill='none' r={radius} stroke={track} strokeWidth={stroke} />
        <circle
          cx={size / 2}
          cy={size / 2}
          fill='none'
          r={radius}
          stroke={color}
          strokeDasharray={circumference}
          strokeDashoffset={circumference * (1 - normalized)}
          strokeLinecap='round'
          strokeWidth={stroke}
        />
      </svg>
      <Stack className='absolute inset-0 items-center justify-center' dataSlot='ring-content' gap='none'>{children}</Stack>
    </Stack>
  )
}

export function StackedAreaChart<T extends object>({
  series,
  keys,
  height = 280,
  labelKey = 'date',
  valueFormatter,
  className
}: {
  series: T[]
  keys: Array<StackedAreaKey<T>>
  height?: number
  labelKey?: string
  valueFormatter?: (value: number) => string
  className?: string
}) {
  const wrapRef = useRef<HTMLDivElement>(null)
  const [hover, setHover] = useState<number | null>(null)
  const width = 720
  const pad = { left: 44, right: 12, top: 14, bottom: 26 }
  const { layers, totals, max, x, y } = useMemo(() => buildStackedAreaLayers({ series, keys, width, height, pad }), [height, keys, series])
  const gridY = [0, 0.25, 0.5, 0.75, 1].map((percent) => ({ percent, value: max * percent, y: pad.top + (height - pad.top - pad.bottom) - percent * (height - pad.top - pad.bottom) }))
  const tickEvery = Math.max(1, Math.ceil(Math.max(1, series.length) / 8))
  const formatValue = valueFormatter ?? ((value) => compact(value))

  function onMove(event: React.MouseEvent<HTMLDivElement>) {
    const rect = wrapRef.current?.getBoundingClientRect()
    if (!rect || series.length === 0) return
    const px = event.clientX - rect.left
    const scaled = (px / rect.width) * width
    const innerWidth = width - pad.left - pad.right
    const index = Math.round(((scaled - pad.left) / innerWidth) * (series.length - 1))
    setHover(Math.max(0, Math.min(series.length - 1, index)))
  }

  return (
    <div className={cn('relative w-full overflow-hidden', className)} onMouseLeave={() => setHover(null)} onMouseMove={onMove} ref={wrapRef}>
      <svg className='block h-auto w-full' role='img' viewBox={`0 0 ${width} ${height}`}>
        <defs>
          {keys.map((key) => (
            <linearGradient id={`area-${key.key}`} key={key.key} x1='0' x2='0' y1='0' y2='1'>
              <stop offset='0%' stopColor={key.color} stopOpacity='.30' />
              <stop offset='100%' stopColor={key.color} stopOpacity='.02' />
            </linearGradient>
          ))}
        </defs>
        {gridY.map((grid, index) => (
          <g key={index}>
            <line stroke='var(--grid-line)' strokeWidth='1' x1={pad.left} x2={width - pad.right} y1={grid.y} y2={grid.y} />
            <text fill='var(--ink-4)' fontFamily='var(--font-mono)' fontSize='10.5' textAnchor='end' x={pad.left - 8} y={grid.y + 3}>
              {formatValue(grid.value)}
            </text>
          </g>
        ))}
        {layers.map((layer) => <path d={layer.area} fill={`url(#area-${layer.key})`} key={`${layer.key}-area`} />)}
        {layers.map((layer) => <path d={layer.path} fill='none' key={`${layer.key}-line`} stroke={layer.color} strokeLinejoin='round' strokeWidth='1.6' />)}
        {series.map((point, index) => index % tickEvery === 0 ? (
          <text fill='var(--ink-4)' fontFamily='var(--font-mono)' fontSize='10.5' key={index} textAnchor='middle' x={x(index)} y={height - 8}>
            {pointLabel(point, labelKey)}
          </text>
        ) : null)}
        {hover != null && series[hover] ? (
          <g>
            <line opacity='.5' stroke='var(--ink-3)' strokeDasharray='3 3' strokeWidth='1' x1={x(hover)} x2={x(hover)} y1={pad.top} y2={height - pad.bottom} />
            <circle cx={x(hover)} cy={y(totals[hover])} fill='var(--surface)' r='3.5' stroke='var(--ai)' strokeWidth='2' />
          </g>
        ) : null}
      </svg>
      {hover != null && series[hover] ? (
        <div
          className='pointer-events-none absolute top-2 min-w-40 rounded-[var(--r-sm)] border border-[var(--line-strong)] bg-[var(--surface)] p-[14px]'
          style={{ left: `min(calc(100% - 180px), max(44px, ${(x(hover) / width) * 100}% + 10px))` }}
        >
          <div className='mono mb-2 text-[11px] text-[var(--ink-3)]'>{pointLabel(series[hover], labelKey)}</div>
          {keys.map((key) => (
            <StackedAreaTooltipRow
              color={key.color}
              key={key.key}
              label={key.label}
              value={formatValue(pointValue(series[hover], key.key))}
            />
          ))}
        </div>
      ) : null}
    </div>
  )
}

export function BarsH({
  rows,
  valueFormatter,
  className
}: {
  rows: Array<{ label: string; value: number; share?: number; color: string }>
  valueFormatter?: (value: number) => string
  className?: string
}) {
  const max = Math.max(1, ...rows.map((row) => row.value))
  const formatValue = valueFormatter ?? ((value) => compact(value))
  return (
    <Stack className={className} dataSlot='bars-h' gap='normal'>
      {rows.map((row) => (
        <Stack dataSlot='bars-h-row' gap='compact' key={row.label}>
          <ActionGroup align='block-end' className='mb-1 gap-3' dataSlot='bars-h-row-header' fit layout='split'>
            <ActionGroup align='start' className='min-w-0 font-medium text-[12px]' dataSlot='bars-h-row-label' fit>
              <span className='size-2 shrink-0 rounded-[3px]' style={{ background: row.color }} />
              <span className='mono truncate'>{row.label}</span>
            </ActionGroup>
            <span className='shrink-0 text-[12px] text-[var(--ink-3)]'>
              <span className='tnum font-semibold text-[var(--ink)]'>{formatValue(row.value)}</span>
              {typeof row.share === 'number' ? <span className='tnum ml-2'>{Math.round(row.share * 100)}%</span> : null}
            </span>
          </ActionGroup>
          <div className='h-[9px] overflow-hidden rounded-[var(--r-full)] bg-[var(--surface-inset)]'>
            <div className='h-full rounded-[var(--r-full)] motion-safe:transition-[width] motion-safe:duration-700 motion-safe:ease-[var(--ease-out)]' style={{ width: `${(row.value / max) * 100}%`, background: row.color }} />
          </div>
        </Stack>
      ))}
    </Stack>
  )
}
