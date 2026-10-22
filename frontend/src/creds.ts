// Карточка подключения: выбор серверов, ручной IP, логин и пароль. Общая для
// всех вкладок. Пароль живёт только здесь, в памяти страницы; на диск, в журнал
// и в URL он не попадает.

import { Go, type Server } from './go'
import { byId, clear, el } from './dom'
import { toast } from './toast'

export class Creds {
  private root: HTMLElement
  private servers: Server[] = []
  private selected = new Set<string>() // выбранные IP
  private manualInput!: HTMLInputElement
  private loginInput!: HTMLInputElement
  private passwordInput!: HTMLInputElement
  private listEl!: HTMLElement

  constructor() {
    this.root = byId('conn')
    this.build()
    void this.load()
  }

  private build(): void {
    clear(this.root)

    const grid = el('div', { class: 'conn-grid' })

    // Левая колонка — серверы + ручной IP
    const left = el('div', { class: 'field' })
    left.append(el('label', { text: 'Серверы' }))
    this.listEl = el('div', { class: 'servers' })
    this.listEl.append(el('div', { class: 'servers-empty', text: 'Загрузка списка…' }))
    left.append(this.listEl)
    this.manualInput = el('input', {
      class: 'inp',
      type: 'text',
      placeholder: 'Или IP вручную, например 192.168.1.10',
      autocomplete: 'off',
      spellcheck: false,
    }) as HTMLInputElement
    const manualField = el('div', { class: 'field' }, [this.manualInput])
    left.append(manualField)

    // Правая колонка — логин и пароль
    const right = el('div', { class: 'field' })
    const loginField = el('div', { class: 'field' })
    loginField.append(el('label', { text: 'Логин' }))
    this.loginInput = el('input', {
      class: 'inp',
      type: 'text',
      placeholder: 'DOMAIN\\admin или .\\localadmin',
      autocomplete: 'off',
      spellcheck: false,
    }) as HTMLInputElement
    loginField.append(this.loginInput)

    const passwordField = el('div', { class: 'field' })
    passwordField.append(el('label', { text: 'Пароль' }))
    this.passwordInput = el('input', {
      class: 'inp',
      type: 'password',
      placeholder: 'Не пишется на диск и в журнал',
      autocomplete: 'off',
    }) as HTMLInputElement
    passwordField.append(this.passwordInput)

    right.append(loginField, passwordField)

    grid.append(left, right)
    this.root.append(grid)
  }

  private async load(): Promise<void> {
    try {
      this.servers = await Go.ServerList()
    } catch (err) {
      this.servers = []
    }
    this.renderList()
  }

  private renderList(): void {
    clear(this.listEl)
    if (this.servers.length === 0) {
      this.listEl.append(
        el('div', { class: 'servers-empty', text: 'Список серверов недоступен — используйте ручной ввод IP.' }),
      )
      return
    }
    for (const server of this.servers) {
      const checkbox = el('input', { type: 'checkbox' }) as HTMLInputElement
      checkbox.checked = this.selected.has(server.ip)
      checkbox.addEventListener('change', () => {
        if (checkbox.checked) this.selected.add(server.ip)
        else this.selected.delete(server.ip)
      })
      const row = el('label', { class: 'server-row' }, [
        checkbox,
        el('span', { class: 'srv-name', text: server.name }),
        el('span', { class: 'srv-ip', text: server.ip }),
      ])
      this.listEl.append(row)
    }
  }

  /** targets возвращает выбранные серверы, либо один ручной IP. */
  targets(): Server[] {
    const chosen = this.servers.filter((s) => this.selected.has(s.ip))
    if (chosen.length > 0) return chosen
    const ip = this.manualInput.value.trim()
    if (ip) return [{ name: 'Ручной ввод', ip } as Server]
    return []
  }

  /** primaryIP — один целевой компьютер для вкладок «Команды» и «Сканер». */
  primaryIP(): string {
    const ip = this.manualInput.value.trim()
    if (ip) return ip
    const chosen = this.servers.filter((s) => this.selected.has(s.ip))
    return chosen.length > 0 ? chosen[0].ip : ''
  }

  get login(): string {
    return this.loginInput.value
  }

  get password(): string {
    return this.passwordInput.value
  }

  /** requireTargets возвращает цели или показывает подсказку и null. */
  requireTargets(): Server[] | null {
    const targets = this.targets()
    if (targets.length === 0) {
      toast('Выберите хотя бы один сервер или введите IP вручную.')
      return null
    }
    return targets
  }

  /** requireIP возвращает один IP или показывает подсказку и пустую строку. */
  requireIP(): string {
    const ip = this.primaryIP()
    if (!ip) toast('Выберите сервер или введите IP вручную.')
    return ip
  }

  /** Очищает пароль (например, при завершении). */
  clearPassword(): void {
    this.passwordInput.value = ''
  }
}
