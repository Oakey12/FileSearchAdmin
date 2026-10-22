// Вкладка «Завершение 1С»: поиск и завершение 1cv7s, список сотрудников и
// оповещение пользователей.

import { Go, onEmployees, type EmployeeProcess, type MessageTemplate } from './go'
import { clear, el } from './dom'
import { confirmAction } from './confirm'
import { toast } from './toast'
import type { Creds } from './creds'

export class MainTab {
  private employees: EmployeeProcess[] = []
  private selected = new Set<number>()
  private listEl!: HTMLElement
  private finishSelectedBtn!: HTMLButtonElement

  constructor(private root: HTMLElement, private creds: Creds) {
    this.build()
    onEmployees((list) => this.showEmployees(list))
  }

  private build(): void {
    clear(this.root)

    // --- Процессы ---
    const proc = el('section', { class: 'panel-card' })
    proc.append(el('h2', { text: 'Процессы 1cv7s' }))
    proc.append(el('p', { class: 'hint', text: 'Действия применяются к выбранным серверам (или к ручному IP).' }))
    const searchBtn = el('button', { class: 'btn', type: 'button', text: 'Найти открытые 1cv7s' })
    searchBtn.addEventListener('click', () => {
      const targets = this.creds.requireTargets()
      if (targets) Go.SearchProcesses(targets, this.creds.login, this.creds.password)
    })
    const finishAllBtn = el('button', { class: 'btn btn-danger', type: 'button', text: 'Завершить 1cv7s у всех на выбранных серверах' })
    finishAllBtn.addEventListener('click', () => this.finishAll())
    proc.append(el('div', { class: 'actions' }, [searchBtn, finishAllBtn]))

    // --- Сотрудники ---
    const emp = el('section', { class: 'panel-card' })
    emp.append(el('h2', { text: 'Сотрудники с открытой 1С' }))
    const scanBtn = el('button', { class: 'btn', type: 'button', text: 'Сканировать сотрудников' })
    scanBtn.addEventListener('click', () => {
      const targets = this.creds.requireTargets()
      if (targets) {
        this.selected.clear()
        Go.ScanEmployees(targets, this.creds.login, this.creds.password)
      }
    })
    this.finishSelectedBtn = el('button', { class: 'btn btn-danger', type: 'button', text: 'Завершить 1С у выбранных' }) as HTMLButtonElement
    this.finishSelectedBtn.disabled = true
    this.finishSelectedBtn.addEventListener('click', () => this.finishSelected())
    emp.append(el('div', { class: 'actions' }, [scanBtn, this.finishSelectedBtn]))
    this.listEl = el('div', { class: 'result-list' })
    this.listEl.append(el('p', { class: 'hint', text: 'Список появится после сканирования.' }))
    emp.append(this.listEl)

    // --- Оповещение ---
    const notify = el('section', { class: 'panel-card' })
    notify.append(el('h2', { text: 'Оповещение пользователей' }))
    const select = el('select', { class: 'inp' }) as HTMLSelectElement
    select.append(el('option', { value: '', text: 'Выберите шаблон…' }))
    const textarea = el('textarea', { class: 'inp', placeholder: 'Текст оповещения…', rows: 3 }) as HTMLTextAreaElement
    textarea.style.minHeight = '72px'
    void this.loadTemplates(select, textarea)
    select.addEventListener('change', () => {
      if (select.value) textarea.value = select.value
    })
    const notifyBtn = el('button', { class: 'btn btn-primary', type: 'button', text: 'Оповестить' })
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
    notify.append(
      el('div', { class: 'field' }, [select]),
      el('div', { class: 'field' }, [textarea]),
      el('div', { class: 'actions' }, [notifyBtn]),
    )

    this.root.append(proc, emp, notify)
  }

  private async loadTemplates(select: HTMLSelectElement, textarea: HTMLTextAreaElement): Promise<void> {
    let templates: MessageTemplate[] = []
    try {
      templates = await Go.NotifyTemplates()
    } catch {
      return
    }
    for (const template of templates) {
      const option = el('option', { value: template.Text, text: template.Name }) as HTMLOptionElement
      select.append(option)
    }
    void textarea
  }

  private async finishAll(): Promise<void> {
    const targets = this.creds.requireTargets()
    if (!targets) return
    const list = targets.map((t) => `• ${t.name} — ${t.ip}`).join('\n')
    const ok = await confirmAction(
      'Опасная операция',
      `Будут завершены ВСЕ процессы 1cv7s.exe на серверах:\n\n${list}\n\nПродолжить?`,
    )
    if (ok) Go.FinishAll(targets, this.creds.login, this.creds.password)
  }

  private showEmployees(list: EmployeeProcess[]): void {
    this.employees = list
    this.selected.clear()
    clear(this.listEl)
    if (list.length === 0) {
      this.listEl.append(el('p', { class: 'hint', text: 'Сотрудники с открытой 1С не найдены.' }))
      this.finishSelectedBtn.disabled = true
      return
    }
    list.forEach((employee, index) => {
      const checkbox = el('input', { type: 'checkbox' }) as HTMLInputElement
      checkbox.addEventListener('change', () => {
        if (checkbox.checked) this.selected.add(index)
        else this.selected.delete(index)
        row.classList.toggle('selected', checkbox.checked)
        this.finishSelectedBtn.disabled = this.selected.size === 0
      })
      const pids = (employee.PIDs ?? []).join(', ')
      const row = el('label', { class: 'result-row' }, [
        checkbox,
        el('div', { class: 'result-main' }, [
          el('div', { class: 'result-name', text: `${employee.Owner} — ${employee.ServerName}` }),
          el('div', { class: 'result-meta', text: `IP ${employee.ServerIP} · PID ${pids}` }),
        ]),
      ])
      this.listEl.append(row)
    })
    this.finishSelectedBtn.disabled = true
  }

  private async finishSelected(): Promise<void> {
    const chosen = [...this.selected].map((i) => this.employees[i]).filter(Boolean)
    if (chosen.length === 0) return
    const ok = await confirmAction('Подтверждение', `Завершить 1С у выбранных сотрудников: ${chosen.length}?`)
    if (ok) Go.FinishEmployees(chosen, this.creds.login, this.creds.password)
  }
}
