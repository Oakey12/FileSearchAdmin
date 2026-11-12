// Категория «Команды»: библиотека PowerShell-скриптов. Можно добавить скрипт
// (имя + текст или .ps1-файл), хранить список и выполнить выбранный скрипт от
// имени администратора на выбранных серверах из JSON или на введённом вручную
// IP. Список скриптов хранится локально у пользователя (localStorage).

import { Go } from './go'
import { clear, el } from './dom'
import { confirmAction } from './confirm'
import { toast } from './toast'
import { categoryHeader, card, actions, button } from './ui'
import type { Creds } from './creds'

interface SavedScript {
  id: string
  name: string
  text: string
}

const STORAGE_KEY = 'admin.commandScripts'

export class CommandsPanel {
  private scripts: SavedScript[] = []
  private selectedId: string | null = null

  private nameInput!: HTMLInputElement
  private textArea!: HTMLTextAreaElement
  private listEl!: HTMLElement
  private runBtn!: HTMLButtonElement
  private selectedLabel!: HTMLElement

  constructor(private root: HTMLElement, private creds: Creds) {
    this.scripts = this.load()
    this.build()
  }

  private build(): void {
    this.root.append(
      categoryHeader(
        'i-play',
        'Команды',
        'Библиотека PowerShell-скриптов: добавьте скрипт и выполните его от имени администратора на выбранных серверах.',
      ),
    )

    // --- Добавить скрипт ---------------------------------------------
    const addCard = card('Добавить скрипт', 'Введите имя и текст скрипта или прикрепите .ps1 (UTF-8, UTF-8 BOM или UTF-16 с BOM).')
    this.nameInput = el('input', {
      class: 'inp',
      type: 'text',
      placeholder: 'Название скрипта, например «Очистка временных файлов»',
      autocomplete: 'off',
      spellcheck: false,
    }) as HTMLInputElement
    this.textArea = el('textarea', {
      class: 'inp',
      placeholder: 'Например: Get-Service | Where-Object Status -eq Running',
      spellcheck: false,
    }) as HTMLTextAreaElement
    this.textArea.style.minHeight = '140px'

    const attachBtn = button('Прикрепить .ps1…')
    attachBtn.addEventListener('click', () => this.attach())
    const addBtn = button('Добавить в список', 'primary')
    addBtn.addEventListener('click', () => this.add())

    addCard.append(
      el('div', { class: 'field' }, [el('label', { text: 'Имя' }), this.nameInput]),
      el('div', { class: 'field' }, [el('label', { text: 'Скрипт' }), this.textArea]),
      actions(attachBtn, addBtn),
    )

    // --- Список скриптов ---------------------------------------------
    const listCard = card('Сохранённые скрипты', 'Выберите один скрипт для запуска. Список хранится на этом компьютере.')
    this.listEl = el('div', { class: 'result-list' })
    listCard.append(this.listEl)

    // --- Выполнение --------------------------------------------------
    const runCard = card('Выполнить от администратора', 'Скрипт запускается на всех выбранных серверах из списка или на введённом вручную IP, от указанных в карточке подключения учётных данных администратора.')
    this.selectedLabel = el('p', { class: 'hint', text: 'Скрипт не выбран.' })
    this.runBtn = button('Выполнить на серверах', 'primary')
    this.runBtn.disabled = true
    this.runBtn.addEventListener('click', () => this.run())
    runCard.append(this.selectedLabel, actions(this.runBtn))

    this.root.append(addCard, listCard, runCard)
    this.renderList()
  }

  private async attach(): Promise<void> {
    try {
      const file = await Go.PickAndReadScript()
      if (!file.name) return
      this.textArea.value = file.text
      if (!this.nameInput.value.trim()) this.nameInput.value = file.name
      toast(`Прикреплён: ${file.name}`)
    } catch (err) {
      toast(String(err))
    }
  }

  private add(): void {
    const name = this.nameInput.value.trim()
    const text = this.textArea.value.trim()
    if (!name) {
      toast('Введите имя скрипта.')
      return
    }
    if (!text) {
      toast('Введите текст скрипта или прикрепите .ps1.')
      return
    }
    const script: SavedScript = { id: newId(), name, text }
    this.scripts.push(script)
    this.save()
    this.nameInput.value = ''
    this.textArea.value = ''
    this.renderList()
    toast(`Скрипт «${name}» добавлен.`)
  }

  private remove(id: string): void {
    this.scripts = this.scripts.filter((s) => s.id !== id)
    if (this.selectedId === id) this.selectedId = null
    this.save()
    this.renderList()
  }

  private renderList(): void {
    clear(this.listEl)
    if (this.scripts.length === 0) {
      this.listEl.append(el('p', { class: 'hint', text: 'Список пуст — добавьте первый скрипт выше.' }))
      this.reflectSelection()
      return
    }
    for (const script of this.scripts) {
      const radio = el('input', { type: 'radio', name: 'cmd-script' }) as HTMLInputElement
      radio.checked = this.selectedId === script.id
      radio.addEventListener('change', () => {
        this.selectedId = script.id
        for (const r of this.listEl.querySelectorAll('.result-row')) r.classList.remove('selected')
        row.classList.add('selected')
        this.reflectSelection()
      })
      const deleteBtn = button('Удалить', 'danger')
      deleteBtn.addEventListener('click', (event) => {
        event.preventDefault()
        event.stopPropagation()
        this.remove(script.id)
      })
      const preview = script.text.length > 120 ? script.text.slice(0, 120) + '…' : script.text
      const row = el('label', { class: 'result-row' + (this.selectedId === script.id ? ' selected' : '') }, [
        radio,
        el('div', { class: 'result-main' }, [
          el('div', { class: 'result-name', text: script.name }),
          el('div', { class: 'result-meta', text: preview.replace(/\s+/g, ' ') }),
        ]),
        deleteBtn,
      ])
      this.listEl.append(row)
    }
    this.reflectSelection()
  }

  private selectedScript(): SavedScript | null {
    return this.scripts.find((s) => s.id === this.selectedId) ?? null
  }

  private reflectSelection(): void {
    const script = this.selectedScript()
    this.runBtn.disabled = !script
    this.selectedLabel.textContent = script ? `Выбран скрипт: «${script.name}».` : 'Скрипт не выбран.'
  }

  private async run(): Promise<void> {
    const script = this.selectedScript()
    if (!script) {
      toast('Выберите скрипт из списка.')
      return
    }
    const targets = this.creds.requireTargets()
    if (!targets) return
    const where = targets.length === 1 ? `на ${targets[0].ip}` : `на серверах: ${targets.length}`
    const ok = await confirmAction(
      'Выполнить от администратора',
      `Запустить скрипт «${script.name}» ${where}?\n\nСкрипт выполнится от учётных данных администратора, указанных в карточке подключения.`,
    )
    if (!ok) return
    Go.RunCommandOnTargets(targets, this.creds.login, this.creds.password, script.text, script.name)
  }

  private load(): SavedScript[] {
    try {
      const raw = localStorage.getItem(STORAGE_KEY)
      if (!raw) return []
      const parsed = JSON.parse(raw) as unknown
      if (!Array.isArray(parsed)) return []
      return parsed
        .filter((s): s is SavedScript => !!s && typeof s.id === 'string' && typeof s.name === 'string' && typeof s.text === 'string')
    } catch {
      return []
    }
  }

  private save(): void {
    try {
      localStorage.setItem(STORAGE_KEY, JSON.stringify(this.scripts))
    } catch {
      /* приватный режим или заполненное хранилище — список не сохранится */
    }
  }
}

function newId(): string {
  try {
    return crypto.randomUUID()
  } catch {
    return `s${Date.now()}-${Math.random().toString(36).slice(2, 8)}`
  }
}
