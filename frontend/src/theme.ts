// Тема интерфейса: тёмная по умолчанию или светлая по выбору человека.
// От темы Windows не зависит. Выбор хранится в localStorage; до первой
// отрисовки его применяет маленький скрипт в index.html, чтобы окно не мигало.

export type Theme = 'dark' | 'light'

/** Ключ в localStorage. Тот же ключ читает встроенный скрипт в index.html. */
export const THEME_KEY = 'theme'

/** Цвет фона окна (--bg) для каждой темы: им закрашивается край окна при растягивании. */
export const WINDOW_BG: Record<Theme, [number, number, number]> = {
  dark: [30, 30, 32],
  light: [245, 245, 247],
}

type Store = Pick<Storage, 'getItem' | 'setItem'>

// Сам доступ к localStorage может бросить исключение — поэтому через функцию.
const defaultStore = (): Store => localStorage

export function loadTheme(store: () => Store = defaultStore): Theme {
  try {
    return store().getItem(THEME_KEY) === 'light' ? 'light' : 'dark'
  } catch {
    return 'dark'
  }
}

export function saveTheme(theme: Theme, store: () => Store = defaultStore): void {
  try { store().setItem(THEME_KEY, theme) } catch { /* без хранилища — тема на один запуск */ }
}

/** Ставит тему на <html>. Тёмная — без атрибута, как и до появления светлой. */
export function applyTheme(theme: Theme, root: HTMLElement = document.documentElement): void {
  if (theme === 'light') root.dataset.theme = 'light'
  else delete root.dataset.theme
}

export function currentTheme(root: HTMLElement = document.documentElement): Theme {
  return root.dataset.theme === 'light' ? 'light' : 'dark'
}

/**
 * Переключает тему мгновенно: на один кадр отключает переходы, иначе
 * каждый элемент с transition начал бы плавно перекрашиваться.
 */
export function switchTheme(theme: Theme, root: HTMLElement = document.documentElement): void {
  root.classList.add('theme-switching')
  applyTheme(theme, root)
  void root.offsetWidth // пересчитать стили, пока переходы выключены
  requestAnimationFrame(() => root.classList.remove('theme-switching'))
}
