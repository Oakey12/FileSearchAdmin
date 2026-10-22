// Категория «О системе»: сбор и показ сведений о целевом компьютере.

import { Go } from './go'
import { clear, el } from './dom'
import { toast } from './toast'
import { categoryHeader, card, actions, button } from './ui'
import type { Creds } from './creds'

export class SystemPanel {
  constructor(private root: HTMLElement, private creds: Creds) {
    this.build()
  }

  private build(): void {
    this.root.append(categoryHeader('i-monitor', 'О системе', 'Сводка о целевом компьютере: ОС, процессор, память, диски, сеть.'))
    const panel = card('Сведения о системе')
    const scanBtn = button('Собрать сведения')
    const copyBtn = button('Копировать')
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
    panel.append(actions(scanBtn, copyBtn), kv)
    this.root.append(panel)
  }
}
