// @vitest-environment happy-dom
import { describe, expect, it } from 'vitest'
import { THEME_KEY, applyTheme, currentTheme, loadTheme, saveTheme } from './theme'

function memoryStore() {
  const data = new Map<string, string>()
  return {
    getItem: (k: string) => data.get(k) ?? null,
    setItem: (k: string, v: string) => { data.set(k, v) },
    data,
  }
}

const broken = () => { throw new Error('нет доступа') }

describe('тема', () => {
  it('по умолчанию тёмная', () => {
    const s = memoryStore()
    expect(loadTheme(() => s)).toBe('dark')
    s.data.set(THEME_KEY, 'что-то странное')
    expect(loadTheme(() => s)).toBe('dark')
  })

  it('сохраняет и читает светлую', () => {
    const s = memoryStore()
    saveTheme('light', () => s)
    expect(s.data.get(THEME_KEY)).toBe('light')
    expect(loadTheme(() => s)).toBe('light')
    saveTheme('dark', () => s)
    expect(loadTheme(() => s)).toBe('dark')
  })

  it('не падает без хранилища', () => {
    expect(loadTheme(broken)).toBe('dark')
    expect(() => saveTheme('light', broken)).not.toThrow()
    const throwing = { getItem: broken, setItem: broken }
    expect(loadTheme(() => throwing)).toBe('dark')
    expect(() => saveTheme('light', () => throwing)).not.toThrow()
  })

  it('ставит и снимает атрибут на <html>', () => {
    const root = document.createElement('html')
    applyTheme('light', root)
    expect(root.getAttribute('data-theme')).toBe('light')
    expect(currentTheme(root)).toBe('light')
    applyTheme('dark', root)
    expect(root.hasAttribute('data-theme')).toBe(false)
    expect(currentTheme(root)).toBe('dark')
  })
})
