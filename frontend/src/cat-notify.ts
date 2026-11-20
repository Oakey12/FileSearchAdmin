// Категория «Оповещения»: рассылка сообщения пользователям на серверах (msg.exe).

import { Go, type MessageTemplate, type Server } from './go'
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
    // Общая проверка перед рассылкой и тестом.
    const prepare = (): { targets: Server[]; message: string } | null => {
      const targets = this.creds.requireTargets()
      const message = textarea.value.trim()
      if (!targets) return null
      if (!message) {
        toast('Введите текст оповещения.')
        return null
      }
      return { targets, message }
    }
    const testBtn = button('Тестовое сообщение')
    testBtn.title = 'Пройти путь рассылки без отправки: в журнале — кому сообщение дошло бы и запускается ли msg.exe на сервере.'
    testBtn.addEventListener('click', () => {
      const ready = prepare()
      if (ready) Go.TestNotify(ready.targets, this.creds.login, this.creds.password, ready.message)
    })
    const notifyBtn = button('Оповестить', 'primary')
    notifyBtn.addEventListener('click', () => {
      const ready = prepare()
      if (ready) Go.Notify(ready.targets, this.creds.login, this.creds.password, ready.message)
    })
    panel.append(
      el('div', { class: 'field' }, [el('label', { text: 'Шаблон' }), dropdown.el]),
      el('div', { class: 'field' }, [el('label', { text: 'Сообщение' }), textarea]),
      actions(testBtn, notifyBtn),
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
