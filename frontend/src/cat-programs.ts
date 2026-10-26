// Категория «Программы»: установленные программы целевого компьютера, поиск по
// имени (по мере набора), удаление и проверка программы по списку компьютеров.

import { Go, type InstalledProgram } from './go'
import { clear, el } from './dom'
import { confirmAction } from './confirm'
import { toast } from './toast'
import { categoryHeader, card, actions, button, filterByName, searchField } from './ui'
import type { Creds } from './creds'
import { AppCheckCard } from './appcheck'

export class ProgramsPanel {
  private programs: InstalledProgram[] = []
  private selected: InstalledProgram | null = null
  private query = ''
  private scanned = false
  private listEl!: HTMLElement
  private uninstallBtn!: HTMLButtonElement
  private countEl!: HTMLElement

  constructor(private root: HTMLElement, private creds: Creds) {
    this.build()
  }

  private build(): void {
    this.root.append(categoryHeader('i-box', 'Программы', 'Установленные программы целевого компьютера и проверка по списку ПК: где программа установлена.'))
    const panel = card('Установленные программы')

    const scanBtn = button('Сканировать программы')
    scanBtn.addEventListener('click', () => this.scan())
    this.uninstallBtn = button('Удалить выбранную', 'danger')
    this.uninstallBtn.disabled = true
    this.uninstallBtn.addEventListener('click', () => this.uninstall())
    panel.append(actions(scanBtn, this.uninstallBtn))

    const search = searchField('Поиск по имени программы…', (value) => {
      this.query = value
      this.renderList()
    })
    panel.append(search.el)

    this.countEl = el('p', { class: 'hint', text: 'Список появится после сканирования.' })
    panel.append(this.countEl)
    this.listEl = el('div', { class: 'result-list' })
    panel.append(this.listEl)
    this.root.append(panel)

    new AppCheckCard(this.root, this.creds)
  }

  private async scan(): Promise<void> {
    const ip = this.creds.requireIP()
    if (!ip) return
    try {
      this.programs = await Go.ScanPrograms(ip, this.creds.login, this.creds.password)
    } catch (err) {
      toast(String(err))
      return
    }
    this.scanned = true
    this.selected = null
    this.uninstallBtn.disabled = true
    this.renderList()
  }

  private renderList(): void {
    clear(this.listEl)
    if (!this.scanned) {
      this.countEl.textContent = 'Список появится после сканирования.'
      return
    }
    const matches = filterByName(this.programs, this.query, (p) => p.Name)
    this.countEl.textContent = this.query.trim()
      ? `Найдено: ${matches.length} из ${this.programs.length}.`
      : `Всего программ: ${this.programs.length}.`

    if (matches.length === 0) {
      this.listEl.append(el('p', { class: 'hint', text: this.query.trim() ? 'Ничего не найдено по запросу.' : 'Программы не найдены.' }))
      return
    }
    for (const program of matches) {
      const radio = el('input', { type: 'radio', name: 'program' }) as HTMLInputElement
      radio.checked = program === this.selected
      radio.addEventListener('change', () => {
        this.selected = program
        this.uninstallBtn.disabled = false
        for (const r of this.listEl.querySelectorAll('.result-row')) r.classList.remove('selected')
        row.classList.add('selected')
      })
      const meta = [program.Version, program.Publisher].filter(Boolean).join(' · ')
      const row = el('label', { class: 'result-row' + (program === this.selected ? ' selected' : '') }, [
        radio,
        el('div', { class: 'result-main' }, [
          el('div', { class: 'result-name', text: program.Name }),
          el('div', { class: 'result-meta', text: meta }),
        ]),
      ])
      this.listEl.append(row)
    }
    // Выбранная программа могла уйти из фильтра — тогда удаление недоступно.
    if (this.selected && !matches.includes(this.selected)) {
      this.selected = null
      this.uninstallBtn.disabled = true
    }
  }

  private async uninstall(): Promise<void> {
    if (!this.selected) return
    const ip = this.creds.requireIP()
    if (!ip) return
    const ok = await confirmAction('Удаление программы', `Удалить «${this.selected.Name}» на ${ip}?`)
    if (!ok) return
    try {
      await Go.UninstallProgram(ip, this.creds.login, this.creds.password, this.selected)
      toast('Команда удаления отправлена. Подробности в журнале.')
    } catch (err) {
      toast(String(err))
    }
  }
}
