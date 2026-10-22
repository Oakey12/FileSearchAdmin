// Ширина и видимость журнала: перетаскивание ресайзера меняет ширину (с
// сохранением), кнопки в шапке и журнале показывают/скрывают панель. Настройки
// хранятся в localStorage и переживают перезапуск.

import { byId } from './dom'

const WIDTH_KEY = 'journalWidth'
const OPEN_KEY = 'journalOpen'
const MIN = 260
const MAX = 680
const DEFAULT = 360

function store(): Storage | null {
  try {
    return window.localStorage
  } catch {
    return null
  }
}

function clampWidth(value: number): number {
  return Math.max(MIN, Math.min(MAX, value))
}

export function loadWidth(get: () => Storage | null = store): number {
  const raw = get()?.getItem(WIDTH_KEY)
  const value = raw ? Number(raw) : NaN
  return Number.isFinite(value) ? clampWidth(value) : DEFAULT
}

export function loadOpen(get: () => Storage | null = store): boolean {
  return get()?.getItem(OPEN_KEY) !== '0'
}

export function setupJournalLayout(): void {
  const admin = byId<HTMLElement>('app')
  const resizer = byId<HTMLElement>('jResizer')
  const toggle = byId<HTMLButtonElement>('journalToggle')
  const collapse = byId<HTMLButtonElement>('jCollapse')

  let width = loadWidth()
  let open = loadOpen()

  const applyWidth = (): void => {
    document.documentElement.style.setProperty('--jw', `${width}px`)
  }
  const applyOpen = (): void => {
    admin.classList.toggle('no-journal', !open)
    toggle.setAttribute('aria-pressed', String(open))
  }
  const saveWidth = (): void => {
    try {
      store()?.setItem(WIDTH_KEY, String(Math.round(width)))
    } catch {
      /* хранилище недоступно */
    }
  }
  const saveOpen = (): void => {
    try {
      store()?.setItem(OPEN_KEY, open ? '1' : '0')
    } catch {
      /* хранилище недоступно */
    }
  }

  applyWidth()
  applyOpen()

  const setOpen = (value: boolean): void => {
    open = value
    applyOpen()
    saveOpen()
  }
  toggle.addEventListener('click', () => setOpen(!open))
  collapse.addEventListener('click', () => setOpen(false))

  // Перетаскивание ресайзера: тянем влево — журнал шире.
  resizer.addEventListener('pointerdown', (event) => {
    event.preventDefault()
    resizer.classList.add('dragging')
    resizer.setPointerCapture(event.pointerId)
    const startX = event.clientX
    const startWidth = width

    const onMove = (move: PointerEvent): void => {
      width = clampWidth(startWidth - (move.clientX - startX))
      applyWidth()
    }
    const onUp = (): void => {
      resizer.classList.remove('dragging')
      resizer.removeEventListener('pointermove', onMove)
      resizer.removeEventListener('pointerup', onUp)
      saveWidth()
    }
    resizer.addEventListener('pointermove', onMove)
    resizer.addEventListener('pointerup', onUp)
  })
}
