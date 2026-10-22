// Категория «Файлы»: просмотр папки, правка текстового файла и удаление.

import { Go, type RemoteFile, type RemoteTextDocument } from './go'
import { clear, el } from './dom'
import { confirmAction } from './confirm'
import { toast } from './toast'
import { categoryHeader, card, actions, button } from './ui'
import type { Creds } from './creds'

export class FilesPanel {
  private files: RemoteFile[] = []
  private selected: RemoteFile | null = null
  private listEl!: HTMLElement
  private deleteBtn!: HTMLButtonElement
  private editBtn!: HTMLButtonElement
  private editorHost!: HTMLElement

  constructor(private root: HTMLElement, private creds: Creds) {
    this.build()
  }

  private build(): void {
    this.root.append(categoryHeader('i-file', 'Файлы', 'Просмотр папки на целевом компьютере, правка текстовых файлов и удаление.'))
    const panel = card('Файлы в папке')
    const pathInput = el('input', { class: 'inp', type: 'text', placeholder: 'Путь к папке, например C:\\Temp', spellcheck: false }) as HTMLInputElement
    const scanBtn = button('Сканировать папку')
    scanBtn.addEventListener('click', () => this.scan(pathInput.value))
    this.editBtn = button('Открыть и редактировать')
    this.editBtn.disabled = true
    this.editBtn.addEventListener('click', () => this.openFile())
    this.deleteBtn = button('Удалить файл', 'danger')
    this.deleteBtn.disabled = true
    this.deleteBtn.addEventListener('click', () => this.deleteFile())
    panel.append(
      el('div', { class: 'field' }, [el('label', { text: 'Папка' }), pathInput]),
      actions(scanBtn, this.editBtn, this.deleteBtn),
    )
    this.listEl = el('div', { class: 'result-list' })
    panel.append(this.listEl)
    this.editorHost = el('div', {})
    this.root.append(panel, this.editorHost)
  }

  private async scan(path: string): Promise<void> {
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
    this.selected = null
    this.editBtn.disabled = true
    this.deleteBtn.disabled = true
    clear(this.listEl)
    if (this.files.length === 0) {
      this.listEl.append(el('p', { class: 'hint', text: 'Файлы не найдены.' }))
      return
    }
    for (const file of this.files) {
      const radio = el('input', { type: 'radio', name: 'file' }) as HTMLInputElement
      radio.addEventListener('change', () => {
        this.selected = file
        this.deleteBtn.disabled = false
        this.editBtn.disabled = !file.IsText
        for (const r of this.listEl.querySelectorAll('.result-row')) r.classList.remove('selected')
        row.classList.add('selected')
      })
      const row = el('label', { class: 'result-row' }, [
        radio,
        el('div', { class: 'result-main' }, [
          el('div', { class: 'result-name', text: file.Name }),
          el('div', { class: 'result-meta', text: `${file.Size} Б · ${file.LastWriteTimeUTC}` }),
        ]),
      ])
      this.listEl.append(row)
    }
  }

  private async openFile(): Promise<void> {
    if (!this.selected) return
    const ip = this.creds.requireIP()
    if (!ip) return
    let doc: RemoteTextDocument
    try {
      doc = await Go.ReadRemoteFile(ip, this.creds.login, this.creds.password, this.selected)
    } catch (err) {
      toast(String(err))
      return
    }
    this.showEditor(ip, doc)
  }

  private showEditor(ip: string, doc: RemoteTextDocument): void {
    clear(this.editorHost)
    const editor = card(`Правка: ${doc.File.Name}`, 'При сохранении создаётся резервная копия оригинала.')
    const area = el('textarea', { class: 'inp', spellcheck: false }) as HTMLTextAreaElement
    area.value = doc.Content
    area.style.minHeight = '260px'
    const saveBtn = button('Сохранить изменения', 'primary')
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
    const closeBtn = button('Закрыть без сохранения')
    closeBtn.addEventListener('click', () => clear(this.editorHost))
    editor.append(el('div', { class: 'field' }, [area]), actions(saveBtn, closeBtn))
    this.editorHost.append(editor)
  }

  private async deleteFile(): Promise<void> {
    if (!this.selected) return
    const ip = this.creds.requireIP()
    if (!ip) return
    const ok = await confirmAction('Удаление файла', `Удалить «${this.selected.Name}» на ${ip}? Действие необратимо.`)
    if (!ok) return
    try {
      await Go.DeleteRemoteFile(ip, this.creds.login, this.creds.password, this.selected)
      toast('Команда удаления отправлена. Подробности в журнале.')
    } catch (err) {
      toast(String(err))
    }
  }
}
