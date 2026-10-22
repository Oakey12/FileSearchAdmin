// Вкладка «Сканер и удаление»: установленные программы, файлы в папке
// (просмотр, правка, удаление) и сведения о системе.

import {
  Go,
  type InstalledProgram,
  type RemoteFile,
  type RemoteTextDocument,
} from './go'
import { clear, el } from './dom'
import { confirmAction } from './confirm'
import { toast } from './toast'
import type { Creds } from './creds'

type Sub = 'programs' | 'files' | 'system'

export class ScannerTab {
  private panels: Record<Sub, HTMLElement> = {} as Record<Sub, HTMLElement>

  // программы
  private programs: InstalledProgram[] = []
  private selectedProgram: InstalledProgram | null = null
  private programsList!: HTMLElement
  private uninstallBtn!: HTMLButtonElement

  // файлы
  private files: RemoteFile[] = []
  private selectedFile: RemoteFile | null = null
  private filesList!: HTMLElement
  private deleteBtn!: HTMLButtonElement
  private editBtn!: HTMLButtonElement
  private editorHost!: HTMLElement

  constructor(private root: HTMLElement, private creds: Creds) {
    this.build()
  }

  private build(): void {
    clear(this.root)

    const seg = el('div', { class: 'seg', role: 'radiogroup', 'aria-label': 'Раздел сканера' })
    const buttons: Record<Sub, HTMLElement> = {
      programs: el('button', { type: 'button', text: 'Программы', 'aria-pressed': 'true' }),
      files: el('button', { type: 'button', text: 'Файлы', 'aria-pressed': 'false' }),
      system: el('button', { type: 'button', text: 'О системе', 'aria-pressed': 'false' }),
    }
    for (const key of Object.keys(buttons) as Sub[]) {
      buttons[key].addEventListener('click', () => this.setSub(key, buttons))
      seg.append(buttons[key])
    }
    this.root.append(seg)

    this.panels.programs = this.buildPrograms()
    this.panels.files = this.buildFiles()
    this.panels.system = this.buildSystem()
    this.panels.files.hidden = true
    this.panels.system.hidden = true
    this.root.append(this.panels.programs, this.panels.files, this.panels.system)
  }

  private setSub(sub: Sub, buttons: Record<Sub, HTMLElement>): void {
    for (const key of Object.keys(buttons) as Sub[]) {
      buttons[key].setAttribute('aria-pressed', String(key === sub))
      this.panels[key].hidden = key !== sub
    }
  }

  // --- Программы ---
  private buildPrograms(): HTMLElement {
    const card = el('section', { class: 'panel-card' })
    card.append(el('h2', { text: 'Установленные программы' }))
    const scanBtn = el('button', { class: 'btn', type: 'button', text: 'Сканировать установленные программы' })
    scanBtn.addEventListener('click', () => this.scanPrograms())
    this.uninstallBtn = el('button', { class: 'btn btn-danger', type: 'button', text: 'Удалить выбранную программу' }) as HTMLButtonElement
    this.uninstallBtn.disabled = true
    this.uninstallBtn.addEventListener('click', () => this.uninstall())
    card.append(el('div', { class: 'actions' }, [scanBtn, this.uninstallBtn]))
    this.programsList = el('div', { class: 'result-list' })
    this.programsList.append(el('p', { class: 'hint', text: 'Список появится после сканирования.' }))
    card.append(this.programsList)
    return card
  }

  private async scanPrograms(): Promise<void> {
    const ip = this.creds.requireIP()
    if (!ip) return
    try {
      this.programs = await Go.ScanPrograms(ip, this.creds.login, this.creds.password)
    } catch (err) {
      toast(String(err))
      return
    }
    this.selectedProgram = null
    this.uninstallBtn.disabled = true
    clear(this.programsList)
    if (this.programs.length === 0) {
      this.programsList.append(el('p', { class: 'hint', text: 'Программы не найдены.' }))
      return
    }
    this.programs.forEach((program) => {
      const radio = el('input', { type: 'radio', name: 'scan-program' }) as HTMLInputElement
      radio.addEventListener('change', () => {
        this.selectedProgram = program
        this.uninstallBtn.disabled = false
        for (const r of this.programsList.querySelectorAll('.result-row')) r.classList.remove('selected')
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
      this.programsList.append(row)
    })
  }

  private async uninstall(): Promise<void> {
    if (!this.selectedProgram) return
    const ip = this.creds.requireIP()
    if (!ip) return
    const ok = await confirmAction('Удаление программы', `Удалить «${this.selectedProgram.Name}» на ${ip}?`)
    if (!ok) return
    try {
      await Go.UninstallProgram(ip, this.creds.login, this.creds.password, this.selectedProgram)
      toast('Команда удаления отправлена. Подробности в журнале.')
    } catch (err) {
      toast(String(err))
    }
  }

  // --- Файлы ---
  private buildFiles(): HTMLElement {
    const card = el('section', { class: 'panel-card' })
    card.append(el('h2', { text: 'Файлы в папке' }))
    const pathInput = el('input', { class: 'inp', type: 'text', placeholder: 'Путь к папке, например C:\\Temp', spellcheck: false }) as HTMLInputElement
    const scanBtn = el('button', { class: 'btn', type: 'button', text: 'Сканировать папку' })
    scanBtn.addEventListener('click', () => this.scanFolder(pathInput.value))
    this.editBtn = el('button', { class: 'btn', type: 'button', text: 'Открыть и редактировать' }) as HTMLButtonElement
    this.editBtn.disabled = true
    this.editBtn.addEventListener('click', () => this.openFile())
    this.deleteBtn = el('button', { class: 'btn btn-danger', type: 'button', text: 'Удалить файл' }) as HTMLButtonElement
    this.deleteBtn.disabled = true
    this.deleteBtn.addEventListener('click', () => this.deleteFile())
    card.append(
      el('div', { class: 'field' }, [pathInput]),
      el('div', { class: 'actions' }, [scanBtn, this.editBtn, this.deleteBtn]),
    )
    this.filesList = el('div', { class: 'result-list' })
    card.append(this.filesList)
    this.editorHost = el('div', {})
    card.append(this.editorHost)
    return card
  }

  private async scanFolder(path: string): Promise<void> {
    const folder = path.trim()
    if (!folder) {
      toast('Укажите путь к папке.')
      return
    }
    const ip = this.creds.requireIP()
    if (!ip) return
    clear(this.editorHost)
    try {
      const result = await Go.ScanFolder(ip, this.creds.login, this.creds.password, folder)
      this.files = result.Files ?? []
    } catch (err) {
      toast(String(err))
      return
    }
    this.selectedFile = null
    this.editBtn.disabled = true
    this.deleteBtn.disabled = true
    clear(this.filesList)
    if (this.files.length === 0) {
      this.filesList.append(el('p', { class: 'hint', text: 'Файлы не найдены.' }))
      return
    }
    this.files.forEach((file) => {
      const radio = el('input', { type: 'radio', name: 'scan-file' }) as HTMLInputElement
      radio.addEventListener('change', () => {
        this.selectedFile = file
        this.deleteBtn.disabled = false
        this.editBtn.disabled = !file.IsText
        for (const r of this.filesList.querySelectorAll('.result-row')) r.classList.remove('selected')
        row.classList.add('selected')
      })
      const row = el('label', { class: 'result-row' }, [
        radio,
        el('div', { class: 'result-main' }, [
          el('div', { class: 'result-name', text: file.Name }),
          el('div', { class: 'result-meta', text: `${file.Size} Б · ${file.LastWriteTimeUTC}` }),
        ]),
      ])
      this.filesList.append(row)
    })
  }

  private async openFile(): Promise<void> {
    if (!this.selectedFile) return
    const ip = this.creds.requireIP()
    if (!ip) return
    let doc: RemoteTextDocument
    try {
      doc = await Go.ReadRemoteFile(ip, this.creds.login, this.creds.password, this.selectedFile)
    } catch (err) {
      toast(String(err))
      return
    }
    this.showEditor(ip, doc)
  }

  private showEditor(ip: string, doc: RemoteTextDocument): void {
    clear(this.editorHost)
    const editor = el('div', { class: 'panel-card' })
    editor.append(el('h2', { text: `Правка: ${doc.File.Name}` }))
    const area = el('textarea', { class: 'inp', spellcheck: false }) as HTMLTextAreaElement
    area.value = doc.Content
    area.style.minHeight = '240px'
    const saveBtn = el('button', { class: 'btn btn-primary', type: 'button', text: 'Сохранить изменения' })
    saveBtn.addEventListener('click', async () => {
      const ok = await confirmAction('Сохранение файла', `Перезаписать «${doc.File.Name}» на ${ip}? Будет создана резервная копия.`, false)
      if (!ok) return
      try {
        await Go.SaveRemoteFile(ip, this.creds.login, this.creds.password, doc, area.value)
        toast('Файл сохранён. Подробности в журнале.')
        clear(this.editorHost)
      } catch (err) {
        toast(String(err))
      }
    })
    const closeBtn = el('button', { class: 'btn', type: 'button', text: 'Закрыть без сохранения' })
    closeBtn.addEventListener('click', () => clear(this.editorHost))
    editor.append(el('div', { class: 'field' }, [area]), el('div', { class: 'actions' }, [saveBtn, closeBtn]))
    this.editorHost.append(editor)
  }

  private async deleteFile(): Promise<void> {
    if (!this.selectedFile) return
    const ip = this.creds.requireIP()
    if (!ip) return
    const ok = await confirmAction('Удаление файла', `Удалить «${this.selectedFile.Name}» на ${ip}? Действие необратимо.`)
    if (!ok) return
    try {
      await Go.DeleteRemoteFile(ip, this.creds.login, this.creds.password, this.selectedFile)
      toast('Команда удаления отправлена. Подробности в журнале.')
    } catch (err) {
      toast(String(err))
    }
  }

  // --- О системе ---
  private buildSystem(): HTMLElement {
    const card = el('section', { class: 'panel-card' })
    card.append(el('h2', { text: 'Сведения о системе' }))
    const scanBtn = el('button', { class: 'btn', type: 'button', text: 'Собрать сведения о системе' })
    const copyBtn = el('button', { class: 'btn', type: 'button', text: 'Копировать' }) as HTMLButtonElement
    copyBtn.disabled = true
    const kv = el('dl', { class: 'kv' })
    scanBtn.addEventListener('click', async () => {
      const ip = this.creds.requireIP()
      if (!ip) return
      try {
        const info = await Go.ScanSystemInfo(ip, this.creds.login, this.creds.password)
        const pairs = await Go.FormatSystemInfo(info)
        clear(kv)
        const text: string[] = []
        for (const pair of pairs as string[][]) {
          const [key, value] = pair
          kv.append(el('dt', { text: key }), el('dd', { text: value }))
          text.push(`${key}: ${value}`)
        }
        copyBtn.disabled = text.length === 0
        copyBtn.onclick = async () => {
          try {
            await navigator.clipboard.writeText(text.join('\n'))
            toast('Сведения скопированы.')
          } catch {
            toast('Не удалось скопировать.')
          }
        }
      } catch (err) {
        toast(String(err))
      }
    })
    card.append(el('div', { class: 'actions' }, [scanBtn, copyBtn]), kv)
    return card
  }
}
