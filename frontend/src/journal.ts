// Панель журнала: принимает строки события journal:line и статус operation:status,
// показывает их, позволяет отменить операцию, скопировать и очистить журнал.

import { Go, onJournalLine, onOperationStatus } from './go'
import { byId, clear, el } from './dom'
import { toast } from './toast'

export class Journal {
  private log = byId<HTMLElement>('jLog')
  private status = byId<HTMLElement>('jStatus')
  private spinner = byId<HTMLElement>('jSpinner')
  private cancelBtn = byId<HTMLButtonElement>('jCancel')
  private lines: string[] = []

  constructor() {
    this.renderEmpty()

    onJournalLine((line) => this.append(line))
    onOperationStatus((s) => this.setBusy(s.running))

    this.cancelBtn.addEventListener('click', () => Go.CancelOperation())
    byId<HTMLButtonElement>('jCopy').addEventListener('click', () => this.copy())
    byId<HTMLButtonElement>('jClear').addEventListener('click', () => this.clearLog())
  }

  private renderEmpty(): void {
    clear(this.log)
    this.log.append(el('div', { class: 'journal-empty', text: 'Здесь появятся сообщения об операциях.' }))
  }

  private append(line: string): void {
    if (this.lines.length === 0) clear(this.log)
    this.lines.push(line)

    let cls = 'journal-line'
    if (line.startsWith('===') || line.startsWith('---')) cls += ' jl-head'
    else if (/ошибк|не удалось|не запущен/i.test(line)) cls += ' jl-error'
    else if (/успешно|отправлен|завершён|выполнен/i.test(line)) cls += ' jl-ok'

    const atBottom = this.log.scrollHeight - this.log.scrollTop - this.log.clientHeight < 40
    this.log.append(el('div', { class: cls, text: line }))
    if (atBottom) this.log.scrollTop = this.log.scrollHeight
  }

  private setBusy(running: boolean): void {
    this.spinner.hidden = !running
    this.cancelBtn.hidden = !running
    this.status.textContent = running ? 'Выполняется…' : 'Журнал'
  }

  private async copy(): Promise<void> {
    if (this.lines.length === 0) return
    try {
      await navigator.clipboard.writeText(this.lines.join('\n'))
      toast('Журнал скопирован.')
    } catch {
      toast('Не удалось скопировать журнал.')
    }
  }

  private clearLog(): void {
    this.lines = []
    this.renderEmpty()
  }
}
