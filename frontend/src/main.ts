import './style.css'
import './admin.css'

import { Go, onUpdateAvailable, onUpdateProgress, type UpdateInfo } from './go'
import {
  WindowSetBackgroundColour,
  WindowSetDarkTheme,
  WindowSetLightTheme,
} from '../wailsjs/runtime/runtime'
import {
  WINDOW_BG,
  applyTheme,
  currentTheme,
  saveTheme,
  switchTheme,
  type Theme,
} from './theme'
import {
  applyBackdrop,
  currentBackdrop,
  mountForest,
  mountParticles,
  saveBackdrop,
  watchSmoothness,
  type Backdrop,
} from './backdrop'
import { byId } from './dom'
import { Creds } from './creds'
import { Journal } from './journal'
import { setupJournalLayout } from './journal-layout'
import { ProcessesPanel } from './cat-processes'
import { NotifyPanel } from './cat-notify'
import { CommandsPanel } from './cat-commands'
import { ProgramsPanel } from './cat-programs'
import { FilesPanel } from './cat-files'
import { SystemPanel } from './cat-system'

// --- Окно: цвет фона под выбранную тему --------------------------------
function syncWindow(theme: Theme): void {
  // Вне Wails (предпросмотр в обычном браузере) runtime недоступен — сбой
  // синхронизации хрома окна не должен ронять интерфейс.
  try {
    const [r, g, b] = WINDOW_BG[theme]
    WindowSetBackgroundColour(r, g, b, 255)
    if (theme === 'light') WindowSetLightTheme()
    else WindowSetDarkTheme()
  } catch {
    /* runtime окна недоступен */
  }
}

// --- Фоны --------------------------------------------------------------
let cleanupScene: (() => void) | null = null
let cleanupSmooth: (() => void) | null = null

function mountScene(backdrop: Backdrop): void {
  cleanupScene?.()
  cleanupScene = null
  cleanupSmooth?.()
  cleanupSmooth = null
  document.documentElement.classList.remove('bd-lite')

  if (backdrop === 'particles') {
    cleanupScene = mountParticles(byId('bdParticles'))
  } else if (backdrop === 'forest') {
    cleanupScene = mountForest(byId('bdForestScene'), byId('bdFireflies'))
  }
  if (backdrop !== 'none') {
    cleanupSmooth = watchSmoothness(() => document.documentElement.classList.add('bd-lite'))
  }
}

function setupTheme(): void {
  const themeToggle = byId<HTMLButtonElement>('themeToggle')

  const reflect = (theme: Theme): void => {
    const light = theme === 'light'
    themeToggle.setAttribute('aria-pressed', String(light))
    themeToggle.title = light ? 'Тёмная тема' : 'Светлая тема'
  }

  reflect(currentTheme())
  syncWindow(currentTheme())

  themeToggle.addEventListener('click', () => {
    // В режиме «Лес» тема форсируется тёмной — переключатель заблокирован.
    if (currentBackdrop() === 'forest') return
    const next: Theme = currentTheme() === 'light' ? 'dark' : 'light'
    switchTheme(next)
    saveTheme(next)
    reflect(next)
    syncWindow(next)
  })
}

function setupBackdrop(): void {
  const button = byId<HTMLButtonElement>('backdropButton')
  const menu = byId<HTMLElement>('backdropMenu')
  const themeToggle = byId<HTMLButtonElement>('themeToggle')

  const options = [...menu.querySelectorAll<HTMLButtonElement>('.bd-option')]

  const reflectOptions = (backdrop: Backdrop): void => {
    for (const option of options) {
      option.setAttribute('aria-checked', String(option.dataset.backdrop === backdrop))
    }
    // «Лес» всегда тёмный: визуально тёмная тема, переключатель выключен.
    if (backdrop === 'forest') {
      applyTheme('dark')
      syncWindow('dark')
      themeToggle.disabled = true
    } else {
      themeToggle.disabled = false
      applyTheme(currentTheme())
      syncWindow(currentTheme())
    }
  }

  const apply = (backdrop: Backdrop): void => {
    applyBackdrop(backdrop)
    saveBackdrop(backdrop)
    mountScene(backdrop)
    reflectOptions(backdrop)
  }

  const closeMenu = (): void => {
    menu.hidden = true
    button.setAttribute('aria-expanded', 'false')
  }

  button.addEventListener('click', (event) => {
    event.stopPropagation()
    const open = menu.hidden
    menu.hidden = !open
    button.setAttribute('aria-expanded', String(open))
  })
  for (const option of options) {
    option.addEventListener('click', () => {
      apply((option.dataset.backdrop as Backdrop) ?? 'none')
      closeMenu()
    })
  }
  document.addEventListener('click', (event) => {
    if (!menu.hidden && !menu.contains(event.target as Node) && event.target !== button) closeMenu()
  })
  document.addEventListener('keydown', (event) => {
    if (event.key === 'Escape' && !menu.hidden) closeMenu()
  })

  const initial = currentBackdrop()
  mountScene(initial)
  reflectOptions(initial)
}

// --- Обновления --------------------------------------------------------
function setupUpdates(): void {
  const button = byId<HTMLButtonElement>('update')
  const dot = byId<HTMLElement>('updateDot')
  const sheet = byId<HTMLDialogElement>('updateSheet')
  let pending: UpdateInfo | null = null

  onUpdateAvailable((info) => {
    if (!info.available) return
    pending = info
    button.hidden = false
    dot.hidden = false
  })

  button.addEventListener('click', () => {
    if (!pending) return
    byId('sheetTitle').textContent = `Версия ${pending.version ?? ''}`
    byId('sheetText').textContent = 'Доступно обновление. Программа закроется и запустится заново.'
    byId('sheetNotes').textContent = pending.notes ?? ''
    byId('sheetError').textContent = ''
    byId<HTMLElement>('sheetProgress').hidden = true
    sheet.showModal()
  })

  onUpdateProgress((fraction) => {
    const wrap = byId<HTMLElement>('sheetProgress')
    wrap.hidden = false
    byId<HTMLElement>('sheetBar').style.width = `${Math.round(fraction * 100)}%`
  })

  byId<HTMLButtonElement>('sheetConfirm').addEventListener('click', async () => {
    byId('sheetError').textContent = ''
    try {
      await Go.InstallUpdate()
    } catch (err) {
      byId('sheetError').textContent = String(err)
    }
  })
}

// --- Категории ---------------------------------------------------------
function setupCategories(creds: Creds): void {
  const items = [...document.querySelectorAll<HTMLButtonElement>('.side-item')]
  const panels = new Map<string, HTMLElement>()
  for (const panel of document.querySelectorAll<HTMLElement>('.cat')) {
    panels.set(panel.dataset.cat ?? '', panel)
  }

  // Панели строятся один раз, состояние сохраняется при переключении категорий.
  new ProcessesPanel(panels.get('processes')!, creds)
  new NotifyPanel(panels.get('notify')!, creds)
  new CommandsPanel(panels.get('commands')!, creds)
  new ProgramsPanel(panels.get('programs')!, creds)
  new FilesPanel(panels.get('files')!, creds)
  new SystemPanel(panels.get('system')!, creds)

  const select = (name: string): void => {
    for (const item of items) item.setAttribute('aria-selected', String(item.dataset.cat === name))
    for (const [key, panel] of panels) panel.hidden = key !== name
  }
  for (const item of items) {
    item.addEventListener('click', () => select(item.dataset.cat ?? 'processes'))
  }
}

// --- Старт -------------------------------------------------------------
async function main(): Promise<void> {
  setupTheme()
  setupBackdrop()
  setupUpdates()

  new Journal()
  setupJournalLayout()
  const creds = new Creds()
  setupCategories(creds)

  try {
    byId('version').textContent = `Версия ${await Go.Version()}`
  } catch {
    /* окно ещё не готово — не критично */
  }
}

void main()
