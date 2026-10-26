// Категория «Программы»: ответ «установлена ли программа» на одном компьютере,
// список установленных программ с фильтром по мере набора, удаление и проверка
// программы по списку компьютеров.

import { Go, type ApplicationCheck, type InstalledProgram } from './go'
import { clear, el } from './dom'
import { confirmAction } from './confirm'
import { toast } from './toast'
import { categoryHeader, card, actions, button, filterByName, searchField } from './ui'
import type { Creds } from './creds'
import { AppCheckCard } from './appcheck'
import { STATUS_LABEL, describeMatch, verdictText } from './appcheck-model'

export class ProgramsPanel {
  private programs: InstalledProgram[] = []
  private selected: InstalledProgram | null = null
  private query = ''
  private scanned = false
  private checking = false
  private listEl!: HTMLElement
  private uninstallBtn!: HTMLButtonElement
  private checkBtn!: HTMLButtonElement
  private countEl!: HTMLElement
  private verdictEl!: HTMLElement

  constructor(private root: HTMLElement, private creds: Creds) {
    this.build()
  }

  private build(): void {
    this.root.append(categoryHeader('i-box', 'Программы', 'Установленные программы целевого компьютера и проверка по списку ПК: где программа установлена.'))
    const panel = card(
      'Программы на компьютере',
      'Впишите название и нажмите «Проверить на ПК» — придёт ответ, установлена ли программа (реестр, службы, процессы, папки установки, файлы). «Сканировать программы» показывает весь список из реестра, а поле его фильтрует.',
    )

    // Поле одно на оба сценария: по мере набора фильтрует список, по Enter
    // или кнопке — спрашивает компьютер напрямую.
    const search = searchField('Название программы, например dr.web или TrueConf', (value) => {
      this.query = value
      this.renderList()
    })
    search.input.addEventListener('keydown', (event) => {
      if (event.key === 'Enter') void this.check()
    })
    this.checkBtn = button('Проверить на ПК', 'primary')
    this.checkBtn.addEventListener('click', () => this.check())
    panel.append(el('div', { class: 'check-query' }, [search.el, this.checkBtn]))

    this.verdictEl = el('div', { class: 'verdict' })
    this.verdictEl.hidden = true
    panel.append(this.verdictEl)

    const scanBtn = button('Сканировать программы')
    scanBtn.addEventListener('click', () => this.scan())
    this.uninstallBtn = button('Удалить выбранную', 'danger')
    this.uninstallBtn.disabled = true
    this.uninstallBtn.addEventListener('click', () => this.uninstall())
    panel.append(actions(scanBtn, this.uninstallBtn))

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
      // Список строится только по реестру: пустой фильтр ещё не значит
      // «не установлена» — подсказываем, как спросить компьютер напрямую.
      const text = this.query.trim()
        ? 'В списке из реестра совпадений нет. Нажмите «Проверить на ПК» — поиск заглянет ещё в службы, процессы и папки установки.'
        : 'Программы не найдены.'
      this.listEl.append(el('p', { class: 'hint', text }))
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

  /** Спрашивает один компьютер, установлена ли программа. */
  private async check(): Promise<void> {
    if (this.checking) return
    const query = this.query.trim()
    if (!query) {
      toast('Впишите название программы.')
      return
    }
    const ip = this.creds.requireIP()
    if (!ip) return

    this.checking = true
    this.checkBtn.disabled = true
    this.showVerdictMessage(`Проверяется «${query}» на ${ip}…`)
    try {
      const result = (await Go.FindApplication(ip, this.creds.login, this.creds.password, query)) as unknown as ApplicationCheck
      this.renderVerdict(result, query)
    } catch (err) {
      // Не началось (занято, нет пароля и т.п.) — причина в журнале.
      this.showVerdictMessage(String(err))
      toast(String(err))
    } finally {
      this.checking = false
      this.checkBtn.disabled = false
    }
  }

  private showVerdictMessage(text: string): void {
    clear(this.verdictEl)
    this.verdictEl.append(el('p', { class: 'hint', text }))
    this.verdictEl.hidden = false
  }

  private renderVerdict(check: ApplicationCheck, query: string): void {
    clear(this.verdictEl)
    this.verdictEl.append(
      el('div', { class: 'verdict-head' }, [
        el('span', { class: `badge st-${check.status}`, text: STATUS_LABEL[check.status] }),
        el('span', { class: 'verdict-text', text: verdictText(check, query) }),
      ]),
    )
    const owners = check.owners ?? []
    if (check.status === 'installed' && owners.length > 0) {
      this.verdictEl.append(el('p', { class: 'hint', text: `У кого: ${owners.join(', ')}` }))
    }
    const matches = check.matches ?? []
    if (matches.length > 0) {
      this.verdictEl.append(el('ul', { class: 'verdict-list' }, matches.map((m) => el('li', { text: describeMatch(m) }))))
    }
    // «Не установлена» при невидимых профилях значит «не видно», а не «нет».
    for (const warning of check.warnings ?? []) {
      this.verdictEl.append(el('p', { class: 'hint', text: `Учтите: ${warning}` }))
    }
    this.verdictEl.hidden = false
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
