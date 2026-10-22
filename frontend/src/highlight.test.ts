// @vitest-environment happy-dom
import { describe, expect, it } from 'vitest'
import { highlightInto, normalize, parseTerms } from './highlight'

describe('разбор запроса', () => {
  it('слова, фразы в кавычках, регистр и ё', () => {
    expect(parseTerms('  Отчёт   "За Июль"  2026 ')).toEqual(['отчет', 'за июль', '2026'])
    expect(normalize('СЧЁТ')).toBe('счет')
  })
})

describe('подсветка', () => {
  it('оборачивает все вхождения в mark и не трогает остальной текст', () => {
    const el = document.createElement('div')
    const marks = highlightInto(el, 'Счёт на оплату, счет №2', ['счет'])
    expect(marks).toHaveLength(2)
    expect(el.textContent).toBe('Счёт на оплату, счет №2')
    expect(marks.map((m) => m.textContent)).toEqual(['Счёт', 'счет'])
  })

  it('не создаёт разметку из текста файла', () => {
    const el = document.createElement('div')
    highlightInto(el, '<img src=x onerror=alert(1)> план', ['план'])
    expect(el.querySelector('img')).toBeNull()
    expect(el.textContent).toContain('<img')
  })

  it('ограничивает число меток', () => {
    const el = document.createElement('div')
    expect(highlightInto(el, 'а '.repeat(50), ['а'], 10)).toHaveLength(10)
  })
})
