import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { HealthFieldItem, HealthFieldList } from './health-field-list'

describe('HealthFieldList', () => {
  test('renders service health rows with semantic status dots', () => {
    const html = renderToStaticMarkup(
      <HealthFieldList>
        <HealthFieldItem label='API' status='healthy' value='ready' />
        <HealthFieldItem label='Worker' status='warning' value='degraded' />
        <HealthFieldItem label='Queue' status='danger' value='blocked' />
      </HealthFieldList>
    )

    expect(html).toContain('data-slot="health-field-list"')
    expect(html).toContain('data-slot="health-field-item"')
    expect(html).toContain('data-slot="health-status-dot"')
    expect(html).toContain('data-status="healthy"')
    expect(html).toContain('data-status="warning"')
    expect(html).toContain('data-status="danger"')
    expect(html).toContain('API')
    expect(html).toContain('ready')
  })
})
