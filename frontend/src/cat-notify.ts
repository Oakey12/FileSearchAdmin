// Категория «Оповещения»: рассылка сообщения пользователям на серверах (msg.exe).

import { Go, type MessageTemplate } from './go'
import { el } from './dom'
import { toast } from './toast'
import { categoryHeader, card, actions, button } from './ui'
import { Dropdown } from './dropdown'
import type { Creds } from './creds'

export class NotifyPanel {
  constructor(private root: HTMLElement, private creds: Creds) {
    this.build()
  }

  private build(): void {
    this.root.append(categoryHeader('i-bell', 'Оповещения', 'Рассылка сообщения всем пользовательским сессиям на выбранных серверах.'))

    const panel = card('Текст оповещения', 'Выберите готовый шаблон или введите свой текст.')
    const textarea = el('textarea', { class: 'inp', placeholder: 'Текст оповещения…' }) as HTMLTextAreaElement
    textarea.style.minHeight = '96px'
    const dropdown = new Dropdown('Выберите шаблон…', (value) => {
      if (value) textarea.value = value
    })
    void this.loadTemplates(dropdown)
    const notifyBtn = button('Оповестить', 'primary')
    notifyBtn.addEventListener('click', () => {
      const targets = this.creds.requireTargets()
      const message = textarea.value.trim()
      if (!targets) return
      if (!message) {
        toast('Введите текст оповещения.')
        return
      }
      Go.Notify(targets, this.creds.login, this.creds.password, message)
    })
    panel.append(
      el('div', { class: 'field' }, [el('label', { text: 'Шаблон' }), dropdown.el]),
      el('div', { class: 'field' }, [el('label', { text: 'Сообщение' }), textarea]),
      actions(notifyBtn),
    )
    this.root.append(panel)
  }

  private async loadTemplates(dropdown: Dropdown): Promise<void> {
    let templates: MessageTemplate[] = []
    try {
      templates = await Go.NotifyTemplates()
    } catch {
      return
    }
    dropdown.setOptions(templates.map((t) => ({ value: t.Text, label: t.Name })))
  }
}
