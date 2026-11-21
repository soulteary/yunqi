type CsvCell = string | number | null | undefined

/** Escape string cells for spreadsheets and preserve numeric cells as numbers. */
function escapeCsvCell(value: CsvCell): string {
  let text = value == null ? '' : String(value)
  if (typeof value === 'string' && /^[\s\uFEFF]*[=+\-@]|^[\t\r\n]/.test(text)) {
    text = "'" + text
  }
  return /[",\r\n]/.test(text) ? '"' + text.replace(/"/g, '""') + '"' : text
}

export function serializeCsv(rows: readonly (readonly CsvCell[])[]): string {
  return rows.map(row => row.map(escapeCsvCell).join(',')).join('\r\n')
}
