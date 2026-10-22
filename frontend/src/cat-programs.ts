// Категория «Программы»: установленные программы целевого компьютера и удаление.

import { Go, type InstalledProgram } from './go'
import { clear, el } from './dom'
import { confirmAction } from './confirm'
import { toast } from './toast'
import { categoryHeader, card, actions, button } from './ui'
import type { Creds } from './creds'

export class ProgramsPanel {
  private programs: InstalledProgram[] = []
  private selected: InstalledProgram | null = null
  private listEl!: HTMLElement
  private uninstallBtn!: HTMLButtonElement

  constructor(private root: HTMLElement, private creds: Creds) {
    this.build()
  }

  private build(): void {
    this.root.append(categoryHeader('i-box', 'Программы', 'Установленные программы целевого компьютера и их удаление.'))
    const panel = card('Установленные программы')
    const scanBtn = button('Сканировать программы')
    scanBtn.addEventListener('click', () => this.scan())
    this.uninstallBtn = button('Удалить выбранную', 'danger')
    this.uninstallBtn.disabled = true
    this.uninstallBtn.addEventListener('click', () => this.uninstall())
    panel.append(actions(scanBtn, this.uninstallBtn))
    this.listEl = el('div', { class: 'result-list' })
    this.listEl.append(el('p', { class: 'hint', text: 'Список появится после сканирования.' }))
    panel.append(this.listEl)
    this.root.append(panel)
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
    this.selected = null
    this.uninstallBtn.disabled = true
    clear(this.listEl)
    if (this.programs.length === 0) {
      this.listEl.append(el('p', { class: 'hint', text: 'Программы не найдены.' }))
      return
    }
    for (const program of this.programs) {
      const radio = el('input', { type: 'radio', name: 'program' }) as HTMLInputElement
      radio.addEventListener('change', () => {
        this.selected = program
        this.uninstallBtn.disabled = false
        for (const r of this.listEl.querySelectorAll('.result-row')) r.classList.remove('selected')
        row.classList.add('selected')
      })
      const meta = [program.Version, program.Publisher].filter(Boolean).join(' · ')
      const row = el('label', { class: 'result-row' }, [
        radio,
        el('div', { class: 'result-main' }, [
          el('div', { class: 'result-name', text: program.Name }),
          el('div', { class: 'result-meta', text: meta }),
        ]),
      ])
      this.listEl.append(row)
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
