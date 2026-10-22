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
