// Категория «Процессы 1С»: поиск и завершение 1cv7s, список сотрудников с
// открытой 1С и завершение у выбранных.

import { Go, onEmployees, type EmployeeProcess } from './go'
import { clear, el } from './dom'
import { confirmAction } from './confirm'
import { categoryHeader, card, actions, button } from './ui'
import type { Creds } from './creds'

export class ProcessesPanel {
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
    this.root.append(categoryHeader('i-gear', 'Процессы 1С', 'Поиск и завершение сеансов 1cv7s на выбранных серверах.'))

    // процессы
    const proc = card('Все процессы на серверах', 'Действия применяются к выбранным серверам или к ручному IP.')
    const searchBtn = button('Найти открытые 1cv7s')
    searchBtn.addEventListener('click', () => {
      const targets = this.creds.requireTargets()
      if (targets) Go.SearchProcesses(targets, this.creds.login, this.creds.password)
    })
    const finishAllBtn = button('Завершить 1cv7s у всех', 'danger')
    finishAllBtn.addEventListener('click', () => this.finishAll())
    proc.append(actions(searchBtn, finishAllBtn))

    // сотрудники
    const emp = card('Сотрудники с открытой 1С', 'Просканируйте, отметьте сотрудников и завершите у них 1С.')
    const scanBtn = button('Сканировать сотрудников')
    scanBtn.addEventListener('click', () => {
      const targets = this.creds.requireTargets()
      if (targets) {
        this.selected.clear()
        Go.ScanEmployees(targets, this.creds.login, this.creds.password)
      }
    })
    this.finishSelectedBtn = button('Завершить 1С у выбранных', 'danger')
    this.finishSelectedBtn.disabled = true
    this.finishSelectedBtn.addEventListener('click', () => this.finishSelected())
    emp.append(actions(scanBtn, this.finishSelectedBtn))
    this.listEl = el('div', { class: 'result-list' })
    this.listEl.append(el('p', { class: 'hint', text: 'Список появится после сканирования.' }))
    emp.append(this.listEl)

    this.root.append(proc, emp)
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
