import { describe, it, expect } from 'vitest'
import { filterByName } from './ui'

interface Prog {
  Name: string
}

const list: Prog[] = [
  { Name: '1С:Предприятие 8.3' },
  { Name: 'Microsoft Office 2019' },
  { Name: 'Google Chrome' },
  { Name: 'Microsoft Edge' },
]
const name = (p: Prog): string => p.Name

describe('filterByName', () => {
  it('пустой запрос возвращает весь список', () => {
    expect(filterByName(list, '', name)).toHaveLength(4)
    expect(filterByName(list, '   ', name)).toHaveLength(4)
  })

  it('ищет по первым буквам без учёта регистра', () => {
    const found = filterByName(list, 'micro', name)
    expect(found.map(name)).toEqual(['Microsoft Office 2019', 'Microsoft Edge'])
  })

  it('ищет по подстроке в любом месте имени', () => {
    expect(filterByName(list, 'office', name).map(name)).toEqual(['Microsoft Office 2019'])
    expect(filterByName(list, 'chrome', name).map(name)).toEqual(['Google Chrome'])
  })

  it('находит кириллицу', () => {
    expect(filterByName(list, '1с', name).map(name)).toEqual(['1С:Предприятие 8.3'])
  })

  it('возвращает пусто, когда совпадений нет', () => {
    expect(filterByName(list, 'photoshop', name)).toHaveLength(0)
  })
})
