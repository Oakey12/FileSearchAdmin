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
  loadTheme,
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
import { toast } from './toast'
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

// Оформление: тема (тёмная / светлая / чёрно-белая) и фон окна. Две связанные
// настройки держим в одном месте, потому что они влияют друг на друга:
//  • «Ч/Б» — лаконичная тема без декоративного фона: фон принудительно «Без фона»
//    и кнопка фона заблокирована;
//  • «Лес» — всегда тёмная сцена: тема отображается тёмной (сохранённый выбор не
//    теряется), а меню тем заблокировано.
function setupAppearance(): void {
  const themeButton = byId<HTMLButtonElement>('themeButton')
  const themeMenu = byId<HTMLElement>('themeMenu')
  const bdButton = byId<HTMLButtonElement>('backdropButton')
  const bdMenu = byId<HTMLElement>('backdropMenu')
  const themeOptions = [...themeMenu.querySelectorAll<HTMLButtonElement>('.th-option')]
  const bdOptions = [...bdMenu.querySelectorAll<HTMLButtonElement>('.bd-option')]

  let theme: Theme = loadTheme()
  let backdrop: Backdrop = currentBackdrop()
  if (theme === 'bw') backdrop = 'none'

  // Отображаемая тема: «Лес» требует тёмной сцены, сам выбор темы при этом цел.
  const shownTheme = (): Theme => (backdrop === 'forest' ? 'dark' : theme)
  const effectiveBackdrop = (): Backdrop => (theme === 'bw' ? 'none' : backdrop)

  const render = (initial = false): void => {
    const shown = shownTheme()
    if (initial) applyTheme(shown)
    else switchTheme(shown)
    syncWindow(shown)

    applyBackdrop(effectiveBackdrop())
    mountScene(effectiveBackdrop())

    for (const option of themeOptions) {
      option.setAttribute('aria-checked', String(option.dataset.theme === theme))
    }
    for (const option of bdOptions) {
      option.setAttribute('aria-checked', String(option.dataset.backdrop === backdrop))
    }

    themeButton.disabled = backdrop === 'forest'
    themeButton.title = backdrop === 'forest' ? 'Тема зафиксирована фоном «Лес»' : 'Тема оформления'
    bdButton.disabled = theme === 'bw'
    bdButton.title = theme === 'bw' ? 'Фон недоступен в чёрно-белой теме' : 'Фон окна'
  }

  const closeMenus = (): void => {
    themeMenu.hidden = true
    themeButton.setAttribute('aria-expanded', 'false')
    bdMenu.hidden = true
    bdButton.setAttribute('aria-expanded', 'false')
  }

  const toggleMenu = (menu: HTMLElement, button: HTMLButtonElement): void => {
    const willOpen = menu.hidden
    closeMenus()
    if (willOpen) {
      menu.hidden = false
      button.setAttribute('aria-expanded', 'true')
    }
  }

  themeButton.addEventListener('click', (event) => {
    event.stopPropagation()
    if (!themeButton.disabled) toggleMenu(themeMenu, themeButton)
  })
  bdButton.addEventListener('click', (event) => {
    event.stopPropagation()
    if (!bdButton.disabled) toggleMenu(bdMenu, bdButton)
  })

  for (const option of themeOptions) {
    option.addEventListener('click', () => {
      theme = (option.dataset.theme as Theme) ?? 'dark'
      saveTheme(theme)
      if (theme === 'bw' && backdrop !== 'none') {
        backdrop = 'none'
        saveBackdrop('none')
      }
      render()
      closeMenus()
    })
  }
  for (const option of bdOptions) {
    option.addEventListener('click', () => {
      backdrop = (option.dataset.backdrop as Backdrop) ?? 'none'
      saveBackdrop(backdrop)
      render()
      closeMenus()
    })
  }

  document.addEventListener('click', (event) => {
    const target = event.target as Node
    if (!themeMenu.hidden && !themeMenu.contains(target) && target !== themeButton) closeMenus()
    else if (!bdMenu.hidden && !bdMenu.contains(target) && target !== bdButton) closeMenus()
  })
  document.addEventListener('keydown', (event) => {
    if (event.key === 'Escape' && (!themeMenu.hidden || !bdMenu.hidden)) closeMenus()
  })

  render(true)
}

// --- Обновления --------------------------------------------------------
function setupUpdates(): void {
  const button = byId<HTMLButtonElement>('update')
  const dot = byId<HTMLElement>('updateDot')
  const label = byId<HTMLElement>('updateLabel')
  const sheet = byId<HTMLDialogElement>('updateSheet')
  let pending: UpdateInfo | null = null

  const openSheet = (info: UpdateInfo): void => {
    byId('sheetTitle').textContent = `Версия ${info.version ?? ''}`
    byId('sheetText').textContent = 'Доступно обновление. Программа закроется и запустится заново.'
    byId('sheetNotes').textContent = info.notes ?? ''
    byId('sheetError').textContent = ''
    byId<HTMLElement>('sheetProgress').hidden = true
    sheet.showModal()
  }

  // Текст кнопки скрыт для глаз (её роль — иконка + красный индикатор), поэтому
  // состояние показываем через title и aria-label, а не видимой надписью.
  const setHint = (text: string): void => {
    label.textContent = text
    button.title = text
    button.setAttribute('aria-label', text)
  }

  const markAvailable = (info: UpdateInfo): void => {
    pending = info
    dot.hidden = false // красный индикатор: доступно обновление
    setHint(`Доступно обновление до ${info.version ?? ''}`)
  }

  // Автопроверка при запуске/по таймеру помечает кнопку, если версия новее.
  onUpdateAvailable((info) => {
    if (info.available) markAvailable(info)
  })

  // Кнопка всегда доступна: если обновление уже найдено — открывает окно
  // установки; иначе проверяет сервер вручную.
  button.addEventListener('click', async () => {
    if (pending) {
      openSheet(pending)
      return
    }
    button.disabled = true
    try {
      const info = await Go.CheckUpdate()
      if (info.available) {
        markAvailable(info)
        openSheet(info)
      } else {
        toast(`Установлена последняя версия (${info.current}).`)
      }
    } catch (err) {
      toast(String(err))
    } finally {
      button.disabled = false
    }
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
  setupAppearance()
  setupUpdates()

  new Journal()
  setupJournalLayout()
  const creds = new Creds()
  setupCategories(creds)

  try {
    const version = `Версия ${await Go.Version()}`
    byId('version').textContent = version
    byId('sideVer').textContent = version
  } catch {
    /* окно ещё не готово — не критично */
  }
}

void main()
