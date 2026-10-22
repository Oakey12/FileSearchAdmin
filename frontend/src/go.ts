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

export function onJournalLine(cb: (line: string) => void): () => void {
  return EventsOn('journal:line', (line: string) => cb(line))
}

export function onOperationStatus(cb: (status: OperationStatus) => void): () => void {
  return EventsOn('operation:status', (status: OperationStatus) => cb(status))
}

export function onEmployees(cb: (list: EmployeeProcess[]) => void): () => void {
  return EventsOn('scan:employees', (list: EmployeeProcess[]) => cb(list ?? []))
}

export function onUpdateAvailable(cb: (info: UpdateInfo) => void): () => void {
  return EventsOn('update:available', (info: UpdateInfo) => cb(info))
}

export function onUpdateProgress(cb: (fraction: number) => void): () => void {
  return EventsOn('update:progress', (fraction: number) => cb(fraction))
}
