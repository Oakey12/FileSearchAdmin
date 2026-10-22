// Свой выпадающий список вместо нативного <select>: нативный список на Windows
// рендерится в стиле ОС и плохо читается. Этот — в теме приложения, с хорошим
// контрастом, клавиатурой и закрытием по клику вне/Esc.

import { clear, el, icon } from './dom'

export interface DropdownOption {
  value: string
  label: string
}

export class Dropdown {
  readonly el: HTMLElement
  private button: HTMLButtonElement
  private menu: HTMLElement
  private label: HTMLElement
  private options: DropdownOption[] = []
  private value = ''

  constructor(
    placeholder: string,
    private onChange: (value: string, label: string) => void,
  ) {
    this.label = el('span', { class: 'dd-label dd-placeholder', text: placeholder })
    this.button = el('button', {
      class: 'dd-button',
      type: 'button',
      'aria-haspopup': 'listbox',
      'aria-expanded': 'false',
    }, [this.label, icon('i-chevron', 'dd-chevron')]) as HTMLButtonElement
    this.menu = el('div', { class: 'dd-menu', role: 'listbox' })
    this.menu.hidden = true
    this.el = el('div', { class: 'dd' }, [this.button, this.menu])

    this.button.addEventListener('click', (event) => {
      event.stopPropagation()
      this.toggle()
    })
    document.addEventListener('click', (event) => {
      if (!this.el.contains(event.target as Node)) this.close()
    })
    this.button.addEventListener('keydown', (event) => {
      if (event.key === 'Escape') this.close()
      else if ((event.key === 'ArrowDown' || event.key === 'Enter' || event.key === ' ') && this.menu.hidden) {
        event.preventDefault()
        this.open()
      }
    })
  }

  setOptions(options: DropdownOption[]): void {
    this.options = options
    clear(this.menu)
    for (const option of options) {
      const item = el('button', {
        class: 'dd-option',
        type: 'button',
        role: 'option',
        'aria-selected': String(option.value === this.value),
      }, [option.label]) as HTMLButtonElement
      item.addEventListener('click', () => {
        this.select(option.value, option.label)
        this.close()
      })
      this.menu.append(item)
    }
  }

  private select(value: string, label: string): void {
    this.value = value
    this.label.textContent = label
    this.label.classList.remove('dd-placeholder')
    for (const item of this.menu.querySelectorAll<HTMLElement>('.dd-option')) {
      const option = this.options.find((o) => o.label === item.textContent)
      item.setAttribute('aria-selected', String(option?.value === value))
    }
    this.onChange(value, label)
  }

  private toggle(): void {
    if (this.menu.hidden) this.open()
    else this.close()
  }

  private open(): void {
    this.menu.hidden = false
    this.button.setAttribute('aria-expanded', 'true')
  }

  private close(): void {
    this.menu.hidden = true
    this.button.setAttribute('aria-expanded', 'false')
  }
}
