// Подсветка найденных слов. Разбор запроса совпадает с Go (internal/search):
// слова через пробел, фраза в кавычках, без учёта регистра, «ё» = «е».

export function normalize(s: string): string {
  return s.toLowerCase().replaceAll('ё', 'е')
}

export function parseTerms(query: string): string[] {
  const terms: string[] = []
  query.split('"').forEach((part, i) => {
    if (i % 2 === 1) {
      const phrase = part.trim()
      if (phrase) terms.push(normalize(phrase))
    } else {
      for (const word of part.split(/\s+/)) if (word) terms.push(normalize(word))
    }
  })
  return terms
}

/**
 * Заполняет el текстом, обернув вхождения слов в <mark>. Возвращает метки.
 * Для кириллицы и латиницы normalize не меняет длину строки, поэтому
 * позиции в нормализованном тексте совпадают с исходными.
 */
export function highlightInto(el: HTMLElement, text: string, terms: string[], maxMarks = 1000): HTMLElement[] {
  const marks: HTMLElement[] = []
  const norm = normalize(text)
  if (!terms.length || norm.length !== text.length) {
    el.textContent = text
    return marks
  }

  // Все вхождения всех слов, по порядку, без пересечений.
  const ranges: [number, number][] = []
  for (const term of terms) {
    for (let at = norm.indexOf(term); at >= 0 && ranges.length < maxMarks * 2; at = norm.indexOf(term, at + term.length)) {
      ranges.push([at, at + term.length])
    }
  }
  ranges.sort((a, b) => a[0] - b[0])

  const fragment = document.createDocumentFragment()
  let pos = 0
  for (const [start, end] of ranges) {
    if (start < pos) continue
    if (marks.length >= maxMarks) break
    if (start > pos) fragment.append(text.slice(pos, start))
    const mark = document.createElement('mark')
    mark.textContent = text.slice(start, end)
    fragment.append(mark)
    marks.push(mark)
    pos = end
  }
  fragment.append(text.slice(pos))
  el.replaceChildren(fragment)
  return marks
}
