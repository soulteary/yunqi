import { describe, expect, it } from 'vitest'
import { serializeCsv } from './csv'

describe('spreadsheet CSV export', () => {
  it('quotes commas, quotes, CR and LF without changing record boundaries', () => {
    expect(serializeCsv([
      ['Model', 'Requests', 'Unpriced models'],
      ['company,model', 1, 'model "quoted"'],
      ['company\nmodel', 2, 'first\rsecond'],
    ])).toBe('Model,Requests,Unpriced models\r\n"company,model",1,"model ""quoted"""\r\n"company\nmodel",2,"first\rsecond"')
  })

  it.each(['=1+1', '+SUM(A1)', '-SUM(A1)', '@SUM(A1)', '  =1+1', '\t=1+1', '\r=1+1', '\n=1+1', '\uFEFF=1+1'])(
    'exports formula-like string %j as text', value => {
      const cell = serializeCsv([[value]])
      expect(cell.startsWith("'") || cell.startsWith(`"'`)).toBe(true)
    },
  )

  it('keeps actual numbers numeric and preserves ordinary text and empty cells', () => {
    expect(serializeCsv([['中文模型', -12, 0, 0.000001, null, undefined, 'gpt-5.6', '']]))
      .toBe('中文模型,-12,0,0.000001,,,gpt-5.6,')
  })
})
