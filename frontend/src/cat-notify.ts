// Категория «Оповещения»: рассылка сообщения пользователям на серверах (msg.exe).

import { Go, type MessageTemplate } from './go'
import { el } from './dom'
import { toast } from './toast'
import { categoryHeader, card, actions, button } from './ui'
import type { Creds } from './creds'

export class NotifyPanel {
  constructor(private root: HTMLElement, private creds: Creds) {
    this.build()
  }

  private build(): void {
    this.root.append(categoryHeader('i-bell', 'Оповещения', 'Рассылка сообщения всем пользовательским сессиям на выбранных серверах.'))

    const panel = card('Текст оповещения', 'Выберите готовый шаблон или введите свой текст.')
    const select = el('select', { class: 'inp' }) as HTMLSelectElement
    select.append(el('option', { value: '', text: 'Выберите шаблон…' }))
    const textarea = el('textarea', { class: 'inp', placeholder: 'Текст оповещения…' }) as HTMLTextAreaElement
    textarea.style.minHeight = '96px'
    void this.loadTemplates(select)
    select.addEventListener('change', () => {
      if (select.value) textarea.value = select.value
    })
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
      el('div', { class: 'field' }, [el('label', { text: 'Шаблон' }), select]),
      el('div', { class: 'field' }, [el('label', { text: 'Сообщение' }), textarea]),
      actions(notifyBtn),
    )
    this.root.append(panel)
  }

  private async loadTemplates(select: HTMLSelectElement): Promise<void> {
    let templates: MessageTemplate[] = []
    try {
      templates = await Go.NotifyTemplates()
    } catch {
      return
    }
    for (const template of templates) {
      select.append(el('option', { value: template.Text, text: template.Name }))
    }
  }
}
