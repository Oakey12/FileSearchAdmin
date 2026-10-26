// Карточка «Проверка программы по списку компьютеров»: загрузка JSON со
// списком IP, название программы и таблица «у какого ПК она установлена».
// Ответы приходят по мере проверки событием appcheck:result.

import { Go, onAppCheckDone, onAppCheckResult, type ApplicationCheck, type Server } from './go'
import { clear, el } from './dom'
import { toast } from './toast'
import { card, actions, button } from './ui'
import type { Creds } from './creds'
import { STATUS_LABEL, countRows, detailsText, rowsToTSV, type CheckRow, type RowStatus } from './appcheck-model'

type Filter = 'all' | 'installed' | 'traces' | 'missing' | 'error'

export class AppCheckCard {
  private computers: Server[] = []
  private duplicates = 0
  private rows: CheckRow[] = []
  private byIP = new Map<string, CheckRow>()
  private running = false
  private filter: Filter = 'all'
  private queryInput!: HTMLInputElement
  private sourceEl!: HTMLElement
  private summaryEl!: HTMLElement
  private bodyEl!: HTMLElement
  private tableWrap!: HTMLElement
  private checkBtn!: HTMLButtonElement
  private stopBtn!: HTMLButtonElement
  private copyBtn!: HTMLButtonElement
  private filterButtons = new Map<Filter, HTMLButtonElement>()

  constructor(private root: HTMLElement, private creds: Creds) {
    this.build()
    onAppCheckResult((check) => this.applyResult(check))
    onAppCheckDone(() => this.finish())
  }

  private build(): void {
    const panel = card(
      'Проверка по списку компьютеров',
      'Загрузите JSON со списком IP, впишите название программы и нажмите «Проверить» — таблица покажет, на каких ПК она установлена.',
    )

    const loadBtn = button('Загрузить JSON…')
    loadBtn.addEventListener('click', () => this.load())
    const sampleBtn = button('Сохранить образец JSON…')
    sampleBtn.title = 'Образец списка: по нему можно собрать свой JSON с любым числом адресов'
    sampleBtn.addEventListener('click', () => this.saveSample())
    panel.append(actions(loadBtn, sampleBtn))

    this.sourceEl = el('p', { class: 'hint' })
    panel.append(this.sourceEl)

    this.queryInput = el('input', {
      class: 'inp',
      type: 'text',
      placeholder: 'Название программы, например TrueConf или 1cv8.exe',
      autocomplete: 'off',
      spellcheck: false,
    }) as HTMLInputElement
    this.queryInput.addEventListener('keydown', (event) => {
      if (event.key === 'Enter') void this.start()
    })
    this.checkBtn = button('Проверить', 'primary')
    this.checkBtn.addEventListener('click', () => this.start())
    this.stopBtn = button('Остановить')
    this.stopBtn.disabled = true
    this.stopBtn.addEventListener('click', () => Go.CancelOperation())
    panel.append(el('div', { class: 'check-query' }, [this.queryInput, this.checkBtn, this.stopBtn]))

    const seg = el('div', { class: 'seg' })
    const filters: [Filter, string][] = [
      ['all', 'Все'],
      ['installed', 'Установлена'],
      ['traces', 'Только файлы'],
      ['missing', 'Не установлена'],
      ['error', 'Ошибки'],
    ]
    for (const [key, label] of filters) {
      const b = el('button', { type: 'button', text: label, 'aria-pressed': String(key === this.filter) }) as HTMLButtonElement
      b.addEventListener('click', () => {
        this.filter = key
        for (const [k, other] of this.filterButtons) other.setAttribute('aria-pressed', String(k === key))
        this.renderBody()
      })
      this.filterButtons.set(key, b)
      seg.append(b)
    }
    this.copyBtn = button('Копировать таблицу')
    this.copyBtn.title = 'Скопировать в буфер — вставляется в Excel по столбцам'
    this.copyBtn.addEventListener('click', () => this.copy())
    this.summaryEl = el('p', { class: 'hint check-summary' })

    this.bodyEl = el('tbody')
    const table = el('table', { class: 'check-table' }, [
      el('thead', {}, [
        el('tr', {}, [
          el('th', { class: 'num', text: '№' }),
          el('th', { text: 'Компьютер' }),
          el('th', { text: 'IP' }),
          el('th', { text: 'Статус' }),
          el('th', { text: 'Подробности' }),
        ]),
      ]),
      this.bodyEl,
    ])
    this.tableWrap = el('div', { class: 'check-result' }, [
      el('div', { class: 'check-toolbar' }, [seg, this.copyBtn]),
      this.summaryEl,
      el('div', { class: 'check-scroll' }, [table]),
    ])
    this.tableWrap.hidden = true
    panel.append(this.tableWrap)

    this.renderSource()
    this.root.append(panel)
  }

  private renderSource(): void {
    this.sourceEl.textContent =
      this.computers.length > 0
        ? `Загружено компьютеров: ${this.computers.length}.` +
          (this.duplicates > 0 ? ` Повторы адресов пропущены: ${this.duplicates}.` : '') +
          ' Чтобы проверить другой список — снова «Загрузить JSON…».'
        :'JSON не загружен — будут проверены серверы, отмеченные в подключении (или IP, введённый вручную).'
  }

  private async load(): Promise<void> {
    try {
      const list = await Go.ImportComputers()
      const computers = list?.computers ?? []
      if (computers.length === 0) return // выбор отменён
      // Новый список заменяет прежний целиком. Таблица прошлой проверки
      // относится к старому списку — убираем её, если проверка не идёт.
      this.computers = computers
      this.duplicates = list.duplicates ?? 0
      if (!this.running) {
        this.rows = []
        this.byIP = new Map()
        this.tableWrap.hidden = true
        this.renderBody()
      }
      this.renderSource()
      toast(
        this.duplicates > 0
          ? `Загружено компьютеров: ${computers.length}. Повторы адресов пропущены: ${this.duplicates}.`
          : `Загружено компьютеров: ${computers.length}.`,
      )
    } catch (err) {
      toast(String(err))
    }
  }

  private async saveSample(): Promise<void> {
    try {
      const path = await Go.SaveComputersExample()
      if (path) toast('Образец сохранён.')
    } catch (err) {
      toast(String(err))
    }
  }

  private async start(): Promise<void> {
    if (this.running) return
    const query = this.queryInput.value.trim()
    if (!query) {
      toast('Введите название программы.')
      this.queryInput.focus()
      return
    }
    const targets = this.computers.length > 0 ? this.computers : this.creds.requireTargets()
    if (!targets) return

    // Таблица готовится до запуска: первые ответы могут прийти сразу.
    const previous = { rows: this.rows, byIP: this.byIP }
    this.rows = targets.map((t) => ({ name: t.name, ip: t.ip, status: 'pending' as RowStatus }))
    this.byIP = new Map(this.rows.map((row) => [row.ip.toLowerCase(), row]))
    this.setRunning(true)
    this.tableWrap.hidden = false
    this.renderBody()

    let started = false
    try {
      started = await Go.CheckApplication(targets, this.creds.login, this.creds.password, query)
    } catch (err) {
      toast(String(err))
    }
    if (!started) {
      // Причина (занято, нет логина и т.п.) уже в журнале; прежняя таблица остаётся.
      this.rows = previous.rows
      this.byIP = previous.byIP
      this.setRunning(false)
      this.tableWrap.hidden = this.rows.length === 0
      this.renderBody()
      toast('Проверка не запущена — причина в журнале.')
    }
  }

  private applyResult(check: ApplicationCheck): void {
    const row = this.byIP.get(check.ip.toLowerCase())
    if (!row) return
    row.status = check.status
    row.check = check
    this.scheduleRender()
  }

  // Ответы приходят пачками по 8 — перерисовка раз в кадр, а не на каждый.
  private renderQueued = false
  private scheduleRender(): void {
    if (this.renderQueued) return
    this.renderQueued = true
    requestAnimationFrame(() => {
      this.renderQueued = false
      this.renderBody()
    })
  }

  private finish(): void {
    for (const row of this.rows) {
      if (row.status === 'pending') row.status = 'skipped'
    }
    this.setRunning(false)
    this.renderBody()
  }

  private setRunning(running: boolean): void {
    this.running = running
    this.checkBtn.disabled = running
    this.stopBtn.disabled = !running
  }

  private renderBody(): void {
    const counts = countRows(this.rows)
    const parts = [
      `Установлена: ${counts.installed}`,
      `не установлена: ${counts.missing}`,
      `ошибок: ${counts.error}`,
    ]
    if (counts.traces > 0) parts.splice(1, 0, `только файлы: ${counts.traces}`)
    if (counts.pending > 0) parts.push(`проверяется: ${counts.pending}`)
    if (counts.skipped > 0) parts.push(`не проверено: ${counts.skipped}`)
    this.summaryEl.textContent = `${parts.join(' · ')}. Всего: ${this.rows.length}.`
    this.copyBtn.disabled = this.rows.length === 0

    clear(this.bodyEl)
    const fragment = document.createDocumentFragment()
    this.rows.forEach((row, index) => {
      if (this.filter !== 'all' && row.status !== this.filter) return
      const details = detailsText(row)
      fragment.append(
        el('tr', {}, [
          el('td', { class: 'num', text: String(index + 1) }),
          el('td', { text: row.name }),
          el('td', { class: 'mono', text: row.ip }),
          el('td', {}, [el('span', { class: `badge st-${row.status}`, text: STATUS_LABEL[row.status] })]),
          el('td', { class: 'details', text: details, title: details }),
        ]),
      )
    })
    this.bodyEl.append(fragment)
  }

  private async copy(): Promise<void> {
    try {
      await navigator.clipboard.writeText(rowsToTSV(this.rows))
      toast('Таблица скопирована.')
    } catch {
      toast('Не удалось скопировать.')
    }
  }
}
