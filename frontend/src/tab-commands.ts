// Вкладка «Команды»: выполнение PowerShell на целевом компьютере или в сессии
// выбранного пользователя.

import { Go, type RemoteUser } from './go'
import { clear, el } from './dom'
import { confirmAction } from './confirm'
import { toast } from './toast'
import type { Creds } from './creds'

type Mode = 'machine' | 'session'

export class CommandsTab {
  private mode: Mode = 'machine'
  private users: RemoteUser[] = []
  private selectedUser: RemoteUser | null = null
  private script!: HTMLTextAreaElement
  private attachedName = 'поле ввода'
  private sessionBlock!: HTMLElement
  private userListEl!: HTMLElement

  constructor(private root: HTMLElement, private creds: Creds) {
    this.build()
  }

  private build(): void {
    clear(this.root)

    const card = el('section', { class: 'panel-card' })
    card.append(el('h2', { text: 'Выполнение PowerShell' }))
    card.append(el('p', { class: 'hint', text: 'Целевой компьютер берётся из поля «IP вручную» или первого выбранного сервера. Логин и пароль — из карточки подключения.' }))

    // режим
    const seg = el('div', { class: 'seg', role: 'radiogroup', 'aria-label': 'Режим' })
    const machineBtn = el('button', { type: 'button', text: 'На компьютере', 'aria-pressed': 'true' })
    const sessionBtn = el('button', { type: 'button', text: 'В сессии пользователя', 'aria-pressed': 'false' })
    machineBtn.addEventListener('click', () => this.setMode('machine', machineBtn, sessionBtn))
    sessionBtn.addEventListener('click', () => this.setMode('session', machineBtn, sessionBtn))
    seg.append(machineBtn, sessionBtn)
    card.append(seg)

    // блок выбора пользователя (для режима сессии)
    this.sessionBlock = el('div', { class: 'field' })
    this.sessionBlock.hidden = true
    const scanUsersBtn = el('button', { class: 'btn', type: 'button', text: 'Сканировать пользователей' })
    scanUsersBtn.addEventListener('click', () => this.scanUsers())
    this.userListEl = el('div', { class: 'result-list' })
    this.sessionBlock.append(el('div', { class: 'actions' }, [scanUsersBtn]), this.userListEl)
    card.append(this.sessionBlock)

    // скрипт
    this.script = el('textarea', { class: 'inp', placeholder: 'Введите команду PowerShell или прикрепите .ps1', spellcheck: false }) as HTMLTextAreaElement
    this.script.addEventListener('input', () => { this.attachedName = 'поле ввода' })
    const attachBtn = el('button', { class: 'btn', type: 'button', text: 'Прикрепить .ps1…' })
    attachBtn.addEventListener('click', () => this.attach())
    const runBtn = el('button', { class: 'btn btn-primary', type: 'button', text: 'Выполнить на целевом компьютере' })
    runBtn.addEventListener('click', () => this.run())

    card.append(
      el('div', { class: 'field' }, [this.script]),
      el('div', { class: 'actions' }, [attachBtn, runBtn]),
    )

    this.root.append(card)
  }

  private setMode(mode: Mode, machineBtn: HTMLElement, sessionBtn: HTMLElement): void {
    this.mode = mode
    machineBtn.setAttribute('aria-pressed', String(mode === 'machine'))
    sessionBtn.setAttribute('aria-pressed', String(mode === 'session'))
    this.sessionBlock.hidden = mode !== 'session'
  }

  private async scanUsers(): Promise<void> {
    const ip = this.creds.requireIP()
    if (!ip) return
    try {
      this.users = await Go.ScanUsers(ip, this.creds.login, this.creds.password)
    } catch (err) {
      toast(String(err))
      return
    }
    this.selectedUser = null
    clear(this.userListEl)
    if (this.users.length === 0) {
      this.userListEl.append(el('p', { class: 'hint', text: 'Активные пользователи не найдены.' }))
      return
    }
    this.users.forEach((user, index) => {
      const radio = el('input', { type: 'radio', name: 'cmd-user' }) as HTMLInputElement
      radio.addEventListener('change', () => {
        this.selectedUser = user
        for (const r of this.userListEl.querySelectorAll('.result-row')) r.classList.remove('selected')
        row.classList.add('selected')
      })
      const row = el('label', { class: 'result-row' }, [
        radio,
        el('div', { class: 'result-main' }, [
          el('div', { class: 'result-name', text: user.DisplayName || user.Username }),
          el('div', { class: 'result-meta', text: `${user.Username} · сессия ${user.SessionID}` }),
        ]),
      ])
      void index
      this.userListEl.append(row)
    })
  }

  private async attach(): Promise<void> {
    try {
      const file = await Go.PickAndReadScript()
      if (!file.name) return
      this.script.value = file.text
      this.attachedName = file.name
      toast(`Прикреплён: ${file.name}`)
    } catch (err) {
      toast(String(err))
    }
  }

  private async run(): Promise<void> {
    const ip = this.creds.requireIP()
    if (!ip) return
    const text = this.script.value.trim()
    if (!text) {
      toast('Введите команду PowerShell или прикрепите .ps1.')
      return
    }
    if (this.mode === 'session' && !this.selectedUser) {
      toast('Выберите пользователя: просканируйте и отметьте сессию.')
      return
    }
    const where = this.mode === 'session' && this.selectedUser
      ? `в сессии «${this.selectedUser.DisplayName || this.selectedUser.Username}» (ID ${this.selectedUser.SessionID})`
      : 'на компьютере'
    const ok = await confirmAction(
      'Выполнить сценарий',
      `Запустить сценарий ${where} на ${ip}?\n\nИсточник: ${this.attachedName}.`,
    )
    if (!ok) return

    if (this.mode === 'session' && this.selectedUser) {
      Go.RunCommandInSession(ip, this.creds.login, this.creds.password, text, this.attachedName, this.selectedUser)
    } else {
      Go.RunCommand(ip, this.creds.login, this.creds.password, text, this.attachedName)
    }
  }
}
