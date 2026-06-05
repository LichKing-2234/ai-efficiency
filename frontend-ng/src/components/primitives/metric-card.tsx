import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'

export function MetricCard({
  label,
  value,
  helper,
  accent = false
}: {
  label: string
  value: React.ReactNode
  helper?: React.ReactNode
  accent?: boolean
}) {
  return (
    <Card className={accent ? 'border-[var(--ae-ai-line)] bg-[linear-gradient(130deg,var(--ae-ai-soft),transparent_55%),var(--card)]' : ''}>
      <CardHeader>
        <CardDescription>{label}</CardDescription>
        <CardTitle className={accent ? 'text-[var(--ae-ai-2)]' : ''}>
          <span className='tnum text-3xl'>{value}</span>
        </CardTitle>
      </CardHeader>
      {helper ? <CardContent className='text-muted-foreground text-xs'>{helper}</CardContent> : null}
    </Card>
  )
}
