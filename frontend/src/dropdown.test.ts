// @vitest-environment happy-dom
import { describe, it, expect, vi } from 'vitest'
import { Dropdown } from './dropdown'

describe('Dropdown', () => {
  it('показывает плейсхолдер до выбора', () => {
    const dd = new Dropdown('Выберите шаблон…', () => {})
    const label = dd.el.querySelector('.dd-label')
    expect(label?.textContent).toBe('Выберите шаблон…')
    expect(label?.classList.contains('dd-placeholder')).toBe(true)
  })

  it('отрисовывает подписи вариантов как текст (без интерпретации разметки)', () => {
    const dd = new Dropdown('Выберите…', () => {})
    dd.setOptions([
      { value: 'a', label: 'Выход из 1С для ручного обмена' },
      { value: 'b', label: '<b>Разметка</b>' },
    ])
    const options = dd.el.querySelectorAll('.dd-option')
    expect(options.length).toBe(2)
    expect(options[0].textContent).toBe('Выход из 1С для ручного обмена')
    // Текст экранируется: HTML не попадает в DOM как разметка.
    expect(options[1].textContent).toBe('<b>Разметка</b>')
    expect(options[1].querySelector('b')).toBeNull()
  })

  it('вызывает onChange и обновляет подпись при выборе', () => {
    const onChange = vi.fn()
    const dd = new Dropdown('Выберите…', onChange)
    dd.setOptions([{ value: 'x', label: 'Первый' }, { value: 'y', label: 'Второй' }])
    const second = dd.el.querySelectorAll('.dd-option')[1] as HTMLButtonElement
    second.click()
    expect(onChange).toHaveBeenCalledWith('y', 'Второй')
    expect(dd.el.querySelector('.dd-label')?.textContent).toBe('Второй')
    expect(second.getAttribute('aria-selected')).toBe('true')
  })

  it('меню скрыто до открытия и открывается по клику на кнопку', () => {
    const dd = new Dropdown('Выберите…', () => {})
    const menu = dd.el.querySelector('.dd-menu') as HTMLElement
    const btn = dd.el.querySelector('.dd-button') as HTMLButtonElement
    expect(menu.hidden).toBe(true)
    btn.click()
    expect(menu.hidden).toBe(false)
    expect(btn.getAttribute('aria-expanded')).toBe('true')
  })
})
