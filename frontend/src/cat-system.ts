// Категория «О системе»: параллельный сбор сведений со всех выбранных
// компьютеров и переключение между ними.

import { Go, onSysInfoResult, onSysInfoDone, type SystemInfoResult } from './go'
import { clear, el } from './dom'
import { toast } from './toast'
import { categoryHeader, card, actions, button } from './ui'
import type { Creds } from './creds'

export class SystemPanel {
  private results = new Map<string, SystemInfoResult>()
  private order: string[] = []
  private pairs = new Map<string, string[][]>() // кэш форматированных пар по IP
  private activeIp = ''
  private tabsEl!: HTMLElement
  private detailEl!: HTMLElement
  private hintEl!: HTMLElement
  private copyBtn!: HTMLButtonElement

  constructor(private root: HTMLElement, private creds: Creds) {
    this.build()
    onSysInfoResult((result) => this.addResult(result))
    onSysInfoDone(() => this.onDone())
  }

  private build(): void {
    this.root.append(categoryHeader('i-monitor', 'О системе', 'Сводка о выбранных компьютерах: ОС, процессор, память, диски, сеть. Переключайтесь между машинами.'))
    const panel = card('Сведения о системе', 'Отмеченные серверы (или ручной IP) опрашиваются параллельно.')
    const scanBtn = button('Собрать сведения')
    scanBtn.addEventListener('click', () => this.scan())
    this.copyBtn = button('Копировать')
    this.copyBtn.disabled = true
    this.copyBtn.addEventListener('click', () => this.copyActive())
    panel.append(actions(scanBtn, this.copyBtn))
    this.hintEl = el('p', { class: 'hint', text: 'Сведения появятся после сбора.' })
    this.tabsEl = el('div', { class: 'sys-tabs' })
    this.detailEl = el('dl', { class: 'kv' })
    panel.append(this.hintEl, this.tabsEl, this.detailEl)
    this.root.append(panel)
  }

  private scan(): void {
    const targets = this.creds.requireTargets()
    if (!targets) return
    this.results.clear()
    this.order = []
    this.pairs.clear()
    this.activeIp = ''
    clear(this.tabsEl)
    clear(this.detailEl)
    this.copyBtn.disabled = true
    this.hintEl.textContent = `Опрашиваю компьютеров: ${targets.length}…`
    Go.ScanSystemInfoAll(targets, this.creds.login, this.creds.password)
  }

  private addResult(result: SystemInfoResult): void {
    const key = result.ip
    if (!this.results.has(key)) this.order.push(key)
    this.results.set(key, result)
    this.renderTabs()
    if (!this.activeIp) void this.select(key)
    this.hintEl.textContent = `Получено ответов: ${this.results.size}.`
  }

  private onDone(): void {
    const errors = [...this.results.values()].filter((r) => r.error).length
    this.hintEl.textContent = errors
      ? `Опрос завершён. Компьютеров: ${this.results.size}, с ошибкой: ${errors}.`
      : `Опрос завершён. Компьютеров: ${this.results.size}.`
  }

  private renderTabs(): void {
    clear(this.tabsEl)
    for (const ip of this.order) {
      const result = this.results.get(ip)
      if (!result) continue
      const label = result.name && result.name !== ip ? result.name : ip
      const cls = 'sys-tab' + (ip === this.activeIp ? ' active' : '') + (result.error ? ' err' : '')
      const tab = el('button', { class: cls, type: 'button', title: ip }, [label])
      tab.addEventListener('click', () => void this.select(ip))
      this.tabsEl.append(tab)
    }
  }

  private async select(ip: string): Promise<void> {
    this.activeIp = ip
    this.renderTabs()
    const result = this.results.get(ip)
    if (!result) return

    if (result.error) {
      clear(this.detailEl)
      this.detailEl.append(el('div', { class: 'sys-error', text: `Не удалось получить сведения: ${result.error}` }))
      this.copyBtn.disabled = true
      return
    }

    let pairs = this.pairs.get(ip)
    if (!pairs) {
      pairs = (await Go.FormatSystemInfo(result.info)) as string[][]
      this.pairs.set(ip, pairs)
    }
    if (this.activeIp !== ip) return // переключились, пока ждали
    clear(this.detailEl)
    for (const pair of pairs) {
      this.detailEl.append(el('dt', { text: pair[0] }), el('dd', { text: pair[1] }))
    }
    this.copyBtn.disabled = pairs.length === 0
  }

  private async copyActive(): Promise<void> {
    const result = this.results.get(this.activeIp)
    const pairs = this.pairs.get(this.activeIp)
    if (!result || !pairs) return
    const header = result.name && result.name !== result.ip ? `${result.name} (${result.ip})` : result.ip
    const text = header + '\n' + pairs.map((pair) => `${pair[0]}: ${pair[1]}`).join('\n')
    try {
      await navigator.clipboard.writeText(text)
      toast('Сведения скопированы.')
    } catch {
      toast('Не удалось скопировать.')
    }
  }
}
