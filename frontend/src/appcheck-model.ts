// Чистая часть проверки по списку компьютеров: статусы строк, текст
// подробностей и выгрузка в TSV. Без DOM — проверяется в vitest.

import type { ApplicationCheck, ApplicationMatch } from './go'

export type RowStatus = ApplicationCheck['status'] | 'pending' | 'skipped'

export interface CheckRow {
  name: string
  ip: string
  status: RowStatus
  check?: ApplicationCheck
}

export const STATUS_LABEL: Record<RowStatus, string> = {
  installed: 'Установлена',
  traces: 'Только файлы',
  missing: 'Не установлена',
  error: 'Ошибка',
  pending: 'Проверяется…',
  skipped: 'Не проверен',
}

/** Текст ячейки «Подробности». */
export function detailsText(row: CheckRow): string {
  const check = row.check
  if (!check) return row.status === 'skipped' ? 'Проверка остановлена до этого компьютера.' : ''
  if (check.status === 'error') return check.error
  if (check.status === 'installed') {
    const owners = check.owners ?? []
    return owners.length > 0 ? `${check.summary} — ${owners.join(', ')}` : check.summary
  }
  if (check.status === 'traces') return `${check.summary} — без записи в реестре, службы и процесса; возможно, остатки после удаления`
  // «Не установлена» с предупреждением значит «не видно», а не «точно нет».
  return (check.warnings ?? []).join('; ')
}

/** Таблица в виде TSV: вставляется в Excel по столбцам. */
export function rowsToTSV(rows: CheckRow[]): string {
  const clean = (value: string): string => value.replace(/[\t\r\n]+/g, ' ').trim()
  const lines = ['Компьютер\tIP\tСтатус\tПодробности']
  for (const row of rows) {
    lines.push([row.name, row.ip, STATUS_LABEL[row.status], detailsText(row)].map(clean).join('\t'))
  }
  return lines.join('\r\n')
}

export function countRows(rows: CheckRow[]): Record<RowStatus, number> {
  const counts: Record<RowStatus, number> = { installed: 0, traces: 0, missing: 0, error: 0, pending: 0, skipped: 0 }
  for (const row of rows) counts[row.status]++
  return counts
}

/** Одна находка поиска строкой: где найдено, что, у кого и по какому пути. */
export function describeMatch(match: ApplicationMatch): string {
  const title = [match.Name, match.Version].filter(Boolean).join(' ')
  const parts = [match.Source ? `${match.Source}: ${title}` : title]
  if (match.Owner) parts.push(match.Owner)
  if (match.Path) parts.push(match.Path)
  return parts.join(' — ')
}

/** Заголовок ответа «установлена или нет» для одного компьютера. */
export function verdictText(check: ApplicationCheck, query: string): string {
  const where = `«${query}» на ${check.ip}`
  if (check.status === 'installed') return `${where}: установлена — ${check.summary}`
  if (check.status === 'traces') return `${where}: найдены только файлы — ${check.summary}; похоже на остатки после удаления`
  if (check.status === 'missing') return `${where}: не установлена`
  return `${where}: проверить не удалось — ${check.error}`
}