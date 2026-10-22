// Единая точка доступа к бэкенду: методы Go и события. Доменные модули
// обращаются сюда, а не к сгенерированным привязкам напрямую.

import * as App from '../wailsjs/go/main/App'
import { EventsOn } from '../wailsjs/runtime/runtime'
import { constandstruct, main } from '../wailsjs/go/models'

// --- Типы данных (из Go) ------------------------------------------------
export type Server = constandstruct.Server
export type EmployeeProcess = constandstruct.EmployeeProcess
export type RemoteUser = constandstruct.RemoteUser
export type InstalledProgram = constandstruct.InstalledProgram
export type RemoteFile = constandstruct.RemoteFile
export type RemoteFolderResult = constandstruct.RemoteFolderResult
export type RemoteTextDocument = constandstruct.RemoteTextDocument
export type RemoteSaveResult = constandstruct.RemoteSaveResult
export type SystemInfo = constandstruct.SystemInfo
export type ApplicationMatch = constandstruct.ApplicationMatch
export type MessageTemplate = constandstruct.MessageTemplate
export type UpdateInfo = main.UpdateInfo

// --- Методы бэкенда -----------------------------------------------------
export const Go = App

// --- События бэкенда ----------------------------------------------------
// Каждая подписка возвращает функцию отписки.

export interface OperationStatus {
  running: boolean
}

// safeOn подписывается на событие Wails. Вне Wails (предпросмотр в браузере)
// runtime отсутствует — тогда возвращается пустая отписка, а интерфейс всё
// равно отрисовывается.
function safeOn(event: string, cb: (...data: unknown[]) => void): () => void {
  try {
    return EventsOn(event, cb)
  } catch {
    return () => {}
  }
}

export function onJournalLine(cb: (line: string) => void): () => void {
  return safeOn('journal:line', (line) => cb(line as string))
}

export function onOperationStatus(cb: (status: OperationStatus) => void): () => void {
  return safeOn('operation:status', (status) => cb(status as OperationStatus))
}

export function onEmployees(cb: (list: EmployeeProcess[]) => void): () => void {
  return safeOn('scan:employees', (list) => cb((list as EmployeeProcess[]) ?? []))
}

export function onUpdateAvailable(cb: (info: UpdateInfo) => void): () => void {
  return safeOn('update:available', (info) => cb(info as UpdateInfo))
}

export function onUpdateProgress(cb: (fraction: number) => void): () => void {
  return safeOn('update:progress', (fraction) => cb(fraction as number))
}
