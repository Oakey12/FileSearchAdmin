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
import { MainTab } from './tab-main'
import { CommandsTab } from './tab-commands'
import { ScannerTab } from './tab-scanner'

// --- Окно: цвет фона под выбранную тему --------------------------------
function syncWindow(theme: Theme): void {
  const [r, g, b] = WINDOW_BG[theme]
  WindowSetBackgroundColour(r, g, b, 255)
  if (theme === 'light') WindowSetLightTheme()
  else WindowSetDarkTheme()
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

// --- Вкладки -----------------------------------------------------------
function setupTabs(creds: Creds): void {
  const tabs = [...document.querySelectorAll<HTMLButtonElement>('.tab')]
  const panels = new Map<string, HTMLElement>()
  for (const panel of document.querySelectorAll<HTMLElement>('.tab-panel')) {
    panels.set(panel.dataset.panel ?? '', panel)
  }

  new MainTab(panels.get('main')!, creds)
  new CommandsTab(panels.get('commands')!, creds)
  new ScannerTab(panels.get('scanner')!, creds)

  const select = (name: string): void => {
    for (const tab of tabs) tab.setAttribute('aria-selected', String(tab.dataset.tab === name))
    for (const [key, panel] of panels) panel.hidden = key !== name
  }
  for (const tab of tabs) {
    tab.addEventListener('click', () => select(tab.dataset.tab ?? 'main'))
  }
}

// --- Старт -------------------------------------------------------------
async function main(): Promise<void> {
  setupTheme()
  setupBackdrop()
  setupUpdates()

  new Journal()
  const creds = new Creds()
  setupTabs(creds)

  try {
    byId('version').textContent = `Версия ${await Go.Version()}`
  } catch {
    /* окно ещё не готово — не критично */
  }
}

void main()
