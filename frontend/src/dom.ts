// Небольшие помощники для императивной сборки DOM. Все пользовательские тексты
// выводятся через textContent, без innerHTML с данными.

export function byId<T extends HTMLElement>(id: string): T {
  const el = document.getElementById(id)
  if (!el) throw new Error(`нет элемента #${id}`)
  return el as T
}

type Attrs = Record<string, string | number | boolean | undefined>

export function el<K extends keyof HTMLElementTagNameMap>(
  tag: K,
  attrs?: Attrs,
  children?: (Node | string)[],
): HTMLElementTagNameMap[K] {
  const node = document.createElement(tag)
  if (attrs) {
    for (const [key, value] of Object.entries(attrs)) {
      if (value === undefined || value === false) continue
      if (key === 'class') node.className = String(value)
      else if (key === 'text') node.textContent = String(value)
      else if (value === true) node.setAttribute(key, '')
      else node.setAttribute(key, String(value))
    }
  }
  if (children) {
    for (const child of children) {
      node.append(typeof child === 'string' ? document.createTextNode(child) : child)
    }
  }
  return node
}

export function clear(node: Node): void {
  while (node.firstChild) node.removeChild(node.firstChild)
}

export function icon(name: string, cls?: string): SVGSVGElement {
  const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg')
  if (cls) svg.setAttribute('class', cls)
  const use = document.createElementNS('http://www.w3.org/2000/svg', 'use')
  use.setAttribute('href', `#${name}`)
  svg.append(use)
  return svg
}
