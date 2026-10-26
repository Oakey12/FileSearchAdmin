import { describe, expect, it } from 'vitest'
import { countRows, describeMatch, detailsText, rowsToTSV, verdictText, type CheckRow } from './appcheck-model'
import type { ApplicationCheck } from './go'

function check(partial: Partial<ApplicationCheck>): ApplicationCheck {
  return { name: '', ip: '', status: 'missing', summary: '', owners: null, matches: null, warnings: null, error: '', ...partial }
}

describe('проверка по списку компьютеров', () => {
  it('подробности зависят от итога', () => {
    const installed: CheckRow = {
      name: 'Бухгалтерия', ip: '10.0.0.1', status: 'installed',
      check: check({ status: 'installed', summary: 'TrueConf 8.5', owners: ['CORP\\ivanov', 'Весь компьютер'] }),
    }
    expect(detailsText(installed)).toBe('TrueConf 8.5 — CORP\\ivanov, Весь компьютер')

    const missing: CheckRow = {
      name: 'Склад', ip: '10.0.0.2', status: 'missing',
      check: check({ warnings: ['Профили без активного сеанса: личные установки не видны у: CORP\\petrov'] }),
    }
    expect(detailsText(missing)).toContain('не видны у')

    const failed: CheckRow = { name: 'ПК', ip: '10.0.0.3', status: 'error', check: check({ status: 'error', error: 'RPC недоступен' }) }
    expect(detailsText(failed)).toBe('RPC недоступен')

    expect(detailsText({ name: 'ПК', ip: '10.0.0.4', status: 'pending' })).toBe('')
    expect(detailsText({ name: 'ПК', ip: '10.0.0.5', status: 'skipped' })).toContain('остановлена')
  })

  it('TSV вставляется в Excel без поломки столбцов', () => {
    const rows: CheckRow[] = [
      { name: 'Бух\tгалтерия', ip: '10.0.0.1', status: 'error', check: check({ status: 'error', error: 'строка 1\r\nстрока 2' }) },
    ]
    const lines = rowsToTSV(rows).split('\r\n')
    expect(lines[0]).toBe('Компьютер\tIP\tСтатус\tПодробности')
    expect(lines[1].split('\t')).toEqual(['Бух галтерия', '10.0.0.1', 'Ошибка', 'строка 1 строка 2'])
  })

  it('считает строки по статусам', () => {
    const counts = countRows([
      { name: 'a', ip: '1', status: 'installed' },
      { name: 'b', ip: '2', status: 'installed' },
      { name: 'c', ip: '3', status: 'pending' },
    ])
    expect(counts.installed).toBe(2)
    expect(counts.pending).toBe(1)
    expect(counts.error).toBe(0)
  })
})

describe('ответ по одному компьютеру', () => {
  it('говорит прямо: установлена, не установлена или ошибка', () => {
    const base = { name: '192.168.140.72', ip: '192.168.140.72' }
    expect(verdictText(check({ ...base, status: 'installed', summary: 'Dr.Web Security Space 12.0 (реестр)' }), 'dr.web'))
      .toBe('«dr.web» на 192.168.140.72: установлена — Dr.Web Security Space 12.0 (реестр)')
    expect(verdictText(check({ ...base, status: 'missing' }), 'dr.web')).toBe('«dr.web» на 192.168.140.72: не установлена')
    expect(verdictText(check({ ...base, status: 'error', error: 'RPC недоступен' }), 'dr.web')).toContain('проверить не удалось — RPC недоступен')
  })

  it('находка показывает источник, владельца и путь', () => {
    expect(describeMatch({ Source: 'служба, работает', Owner: 'Весь компьютер', Name: 'Dr.Web Scanning Engine', Version: '', Path: 'C:\\Program Files\\DrWeb\\dwengine.exe' }))
      .toBe('служба, работает: Dr.Web Scanning Engine — Весь компьютер — C:\\Program Files\\DrWeb\\dwengine.exe')
    expect(describeMatch({ Source: 'реестр', Owner: '', Name: 'Dr.Web', Version: '12.0', Path: '' })).toBe('реестр: Dr.Web 12.0')
  })
})

describe('только файлы', () => {
  it('не выдаётся за установленную программу', () => {
    const traces = check({ name: 'ПК', ip: '10.0.0.9', status: 'traces', summary: 'DrWeb (папка)' })
    expect(verdictText(traces, 'dr.web')).toBe('«dr.web» на 10.0.0.9: найдены только файлы — DrWeb (папка); похоже на остатки после удаления')
    expect(detailsText({ name: 'ПК', ip: '10.0.0.9', status: 'traces', check: traces })).toContain('остатки после удаления')
    expect(countRows([{ name: 'ПК', ip: '10.0.0.9', status: 'traces' }]).traces).toBe(1)
  })
})