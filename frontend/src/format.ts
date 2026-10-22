// Форматирование чисел, дат, размеров и типов файлов.

const numberFormat = new Intl.NumberFormat('ru-RU')
const decimalFormat = new Intl.NumberFormat('ru-RU', { maximumFractionDigits: 1 })
const timeFormat = new Intl.DateTimeFormat('ru-RU', { hour: '2-digit', minute: '2-digit' })
const dateFormat = new Intl.DateTimeFormat('ru-RU', {
  day: '2-digit', month: '2-digit', year: 'numeric', hour: '2-digit', minute: '2-digit',
})
const longDateFormat = new Intl.DateTimeFormat('ru-RU', {
  day: 'numeric', month: 'long', year: 'numeric', hour: '2-digit', minute: '2-digit',
})

export const collator = new Intl.Collator('ru', { numeric: true, sensitivity: 'base' })

export function formatCount(n: number): string {
  return numberFormat.format(n)
}

// plural(5, 'файл', 'файла', 'файлов') → 'файлов'
export function plural(n: number, one: string, few: string, many: string): string {
  const mod10 = n % 10
  const mod100 = n % 100
  if (mod10 === 1 && mod100 !== 11) return one
  if (mod10 >= 2 && mod10 <= 4 && (mod100 < 12 || mod100 > 14)) return few
  return many
}

export function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} байт`
  const units = ['КБ', 'МБ', 'ГБ', 'ТБ']
  let value = bytes / 1024
  let unit = 0
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024
    unit++
  }
  return `${decimalFormat.format(value)} ${units[unit]}`
}

let todayStart = 0
let todayCheckedAt = 0

function startOfToday(): number {
  const now = Date.now()
  if (now - todayCheckedAt > 60_000) {
    const d = new Date(now)
    d.setHours(0, 0, 0, 0)
    todayStart = d.getTime()
    todayCheckedAt = now
  }
  return todayStart
}

// Как в Finder: «Сегодня, 14:03», «Вчера, 09:12», иначе полная дата.
export function formatDate(ms: number): string {
  const today = startOfToday()
  if (ms >= today) return `Сегодня, ${timeFormat.format(ms)}`
  if (ms >= today - 86_400_000) return `Вчера, ${timeFormat.format(ms)}`
  return dateFormat.format(ms)
}

export function formatLongDate(ms: number): string {
  return longDateFormat.format(ms)
}

export function seconds(ms: number): string {
  return ms < 1000 ? `${ms} мс` : `${decimalFormat.format(ms / 1000)} с`
}

// --- Типы файлов -------------------------------------------------------

export type Category =
  | 'doc' | 'sheet' | 'slides' | 'pdf' | 'image' | 'archive' | 'code' | 'media' | 'app' | 'other'

interface KindInfo { label: string; category: Category }

const kinds: Record<string, KindInfo> = {}

function define(category: Category, label: string, exts: string[]) {
  for (const ext of exts) kinds[ext] = { label, category }
}

define('doc', 'Документ Word', ['doc', 'docx', 'rtf', 'odt'])
define('doc', 'Текст', ['txt', 'log', 'md'])
define('sheet', 'Таблица Excel', ['xls', 'xlsx', 'xlsm', 'xltx', 'xltm', 'ods'])
define('sheet', 'Таблица CSV', ['csv', 'tsv'])
define('slides', 'Презентация', ['ppt', 'pptx', 'odp'])
define('pdf', 'Документ PDF', ['pdf'])
define('image', 'Изображение', ['png', 'jpg', 'jpeg', 'gif', 'bmp', 'webp', 'ico', 'tif', 'tiff', 'heic', 'svg'])
define('archive', 'Архив', ['zip', 'rar', '7z', 'tar', 'gz', 'cab'])
define('code', 'Код', ['go', 'js', 'ts', 'py', 'cs', 'java', 'c', 'h', 'cpp', 'php', 'sql', 'css', 'html', 'htm', 'ps1', 'bat', 'cmd', 'sh'])
define('code', 'Данные', ['json', 'xml', 'yaml', 'yml', 'ini', 'conf', 'cfg', 'toml'])
define('media', 'Аудио', ['mp3', 'wav', 'flac', 'ogg', 'm4a'])
define('media', 'Видео', ['mp4', 'mov', 'avi', 'mkv', 'wmv'])
define('app', 'Программа', ['exe', 'msi', 'dll', 'lnk'])

export function kindOf(ext: string): KindInfo {
  return kinds[ext] ?? { label: ext ? `Файл ${ext.toUpperCase()}` : 'Файл', category: 'other' }
}

export function baseName(path: string): string {
  const trimmed = path.replace(/[\\/]+$/, '')
  const i = Math.max(trimmed.lastIndexOf('\\'), trimmed.lastIndexOf('/'))
  return i >= 0 ? trimmed.slice(i + 1) || trimmed : trimmed
}
