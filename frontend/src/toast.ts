// Короткое уведомление внизу окна, при необходимости с действием («Отменить»).

const box = document.getElementById('toast') as HTMLDivElement
const text = document.getElementById('toastText') as HTMLSpanElement
const action = document.getElementById('toastAction') as HTMLButtonElement

let timer = 0
let onAction: (() => void) | null = null

action.addEventListener('click', () => {
  const fn = onAction
  hide()
  fn?.()
})

function hide(): void {
  box.hidden = true
  onAction = null
  clearTimeout(timer)
}

export function toast(message: string, actionLabel?: string, fn?: () => void): void {
  text.textContent = message
  action.hidden = !actionLabel
  action.textContent = actionLabel ?? ''
  onAction = fn ?? null
  box.hidden = false
  clearTimeout(timer)
  timer = window.setTimeout(hide, actionLabel ? 6000 : 3500)
}
