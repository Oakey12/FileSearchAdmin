import { describe, it, expect } from 'vitest'
import { loadWidth, loadOpen } from './journal-layout'

function fakeStore(map: Record<string, string>): () => Storage {
  return () =>
    ({
      getItem: (key: string) => (key in map ? map[key] : null),
      setItem: () => {},
      removeItem: () => {},
      clear: () => {},
      key: () => null,
      length: 0,
    }) as unknown as Storage
}

describe('ширина журнала', () => {
  it('по умолчанию 360, когда ничего не сохранено', () => {
    expect(loadWidth(fakeStore({}))).toBe(360)
  })
  it('зажимается снизу до 260', () => {
    expect(loadWidth(fakeStore({ journalWidth: '100' }))).toBe(260)
  })
  it('зажимается сверху до 680', () => {
    expect(loadWidth(fakeStore({ journalWidth: '9999' }))).toBe(680)
  })
  it('читает корректное значение', () => {
    expect(loadWidth(fakeStore({ journalWidth: '420' }))).toBe(420)
  })
  it('игнорирует мусор и берёт значение по умолчанию', () => {
    expect(loadWidth(fakeStore({ journalWidth: 'abc' }))).toBe(360)
  })
})

describe('видимость журнала', () => {
  it('по умолчанию открыт', () => {
    expect(loadOpen(fakeStore({}))).toBe(true)
  })
  it('скрыт, когда сохранено 0', () => {
    expect(loadOpen(fakeStore({ journalOpen: '0' }))).toBe(false)
  })
  it('открыт, когда сохранено 1', () => {
    expect(loadOpen(fakeStore({ journalOpen: '1' }))).toBe(true)
  })
})
