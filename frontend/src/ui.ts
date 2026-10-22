// Общие помощники для панелей категорий: заголовок категории и карточка.

import { el, icon } from './dom'

/** Шапка категории: иконка, заголовок, подзаголовок. */
export function categoryHeader(iconName: string, title: string, subtitle: string): HTMLElement {
  return el('header', { class: 'cat-head' }, [
    icon(iconName, 'cat-head-icon'),
    el('div', {}, [
      el('h1', { class: 'cat-title', text: title }),
      el('p', { class: 'cat-sub', text: subtitle }),
    ]),
  ])
}

/** Карточка-раздел с заголовком и необязательной подсказкой. */
export function card(title: string, hint?: string): HTMLElement {
  const node = el('section', { class: 'panel-card' })
  node.append(el('h2', { text: title }))
  if (hint) node.append(el('p', { class: 'hint', text: hint }))
  return node
}

/** Строка кнопок. */
export function actions(...buttons: HTMLElement[]): HTMLElement {
  return el('div', { class: 'actions' }, buttons)
}

/** Кнопка. variant: '', 'primary', 'danger'. */
export function button(label: string, variant: '' | 'primary' | 'danger' = ''): HTMLButtonElement {
  const cls = variant ? `btn btn-${variant}` : 'btn'
  return el('button', { class: cls, type: 'button', text: label }) as HTMLButtonElement
}

/** Фильтрует список по подстроке запроса без учёта регистра. Пустой запрос —
 *  весь список. Используется для поиска по мере набора. */
export function filterByName<T>(items: T[], query: string, name: (item: T) => string): T[] {
  const q = query.trim().toLowerCase()
  if (!q) return items
  return items.filter((item) => name(item).toLowerCase().includes(q))
}

/** Поле поиска с иконкой. oninput вызывается по мере набора. */
export function searchField(placeholder: string, oninput: (value: string) => void): { el: HTMLElement; input: HTMLInputElement } {
  const input = el('input', { class: 'search-input', type: 'search', placeholder, spellcheck: false, autocomplete: 'off' }) as HTMLInputElement
  input.addEventListener('input', () => oninput(input.value))
  const box = el('div', { class: 'search-box' }, [icon('i-search', 'search-icon'), input])
  return { el: box, input }
}
