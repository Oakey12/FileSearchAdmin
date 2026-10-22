// Диалог подтверждения необратимых действий (удаление, массовое завершение,
// запуск произвольного сценария).

import { byId } from './dom'

export function confirmAction(title: string, text: string, danger = true): Promise<boolean> {
  const sheet = byId<HTMLDialogElement>('confirmSheet')
  byId('confirmTitle').textContent = title
  byId('confirmText').textContent = text
  const ok = byId<HTMLButtonElement>('confirmOk')
  ok.className = danger ? 'btn btn-danger' : 'btn btn-primary'

  return new Promise((resolve) => {
    let confirmed = false
    const onOk = (): void => {
      confirmed = true
      sheet.close()
    }
    const onClose = (): void => {
      ok.removeEventListener('click', onOk)
      sheet.removeEventListener('close', onClose)
      resolve(confirmed)
    }
    ok.addEventListener('click', onOk)
    sheet.addEventListener('close', onClose)
    sheet.showModal()
  })
}
