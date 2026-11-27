# Разработка на другом компьютере

Всё нужное для сборки лежит в репозитории: исходники Go и TypeScript, готовый
интерфейс `frontend/dist`, привязки `frontend/wailsjs`, `package-lock.json`,
`go.sum`. Не хранятся только то, что скачивается или собирается заново:
`frontend/node_modules/`, `build/bin/` и сами `.exe`.

## Быстрый старт

1. Установите Git, Go и Node.js (один раз):

   ```powershell
   winget install Git.Git GoLang.Go OpenJS.NodeJS.LTS
   ```

   Откройте **новое** окно PowerShell, чтобы подхватились пути.

2. Скачайте проект и подготовьте его:

   ```powershell
   git clone https://github.com/Oakey12/FileSearchAdmin.git
   cd FileSearchAdmin
   .\tools\setup-dev.ps1
   ```

   Скрипт проверит Git/Go/Node, поставит Wails CLI, скачает модули Go и npm,
   прогонит тесты и соберёт `build\bin\filesearch-admin-dev.exe`.
   Если чего-то не хватает, запустите `.\tools\setup-dev.ps1 -Install` — он
   поставит недостающее через winget.

3. Работайте:

   | Что | Команда |
   |---|---|
   | Запуск с живой перезагрузкой интерфейса | `wails dev` |
   | Тесты Go | `go test ./...` |
   | Тесты интерфейса | `cd frontend; npm test` |
   | Пересобрать интерфейс (после правок в `frontend/src`) | `cd frontend; npm run build` |
   | Собрать exe | `wails build -s -skipbindings -o filesearch-admin-dev.exe` |

## Версии, на которых проект собирается

| Инструмент | Версия |
|---|---|
| Go | 1.25+ (на рабочей машине 1.27.3) |
| Wails CLI | v2.16.0 |
| Node.js | 24 LTS (npm 11) |
| WebView2 | есть в Windows 10/11 |

Wails CLI ставится так: `go install github.com/wailsapp/wails/v2/cmd/wails@v2.16.0`
(попадает в `%USERPROFILE%\go\bin`).

## Правила репозитория

- **`frontend/dist` и `frontend/wailsjs` коммитятся.** Сервер публикации
  собирает без npm и без интернета (`wails build -s -skipbindings`). После
  правок в `frontend/src` выполните `npm run build` и закоммитьте `dist`.
- **Привязки `frontend/wailsjs/go/main/App.js` и `App.d.ts` правятся вручную**
  при добавлении метода `App` (алфавитный порядок, как генерирует Wails).
  Либо один раз `wails generate module` и проверить diff.
- **Пароль администратора никогда не пишется** на диск, в журнал или в URL:
  он передаётся в PowerShell через stdin (base64).
- Сообщения коммитов — по-русски, в стиле `feat(область): что сделано`.
- `.ps1` с кириллицей сохраняются в **UTF-8 с BOM**, иначе Windows
  PowerShell 5.1 читает их как ANSI и падает.

## Устройство

- `main.go`, `app*.go` — методы `App`, которые вызывает интерфейс; события
  `journal:line`, `operation:status` и др. идут в интерфейс через `runtime.EventsEmit`.
- `internal/ops` — операции (закрытие 1С, оповещения, тест подключения, сканы).
  Тестовые подмены: `runRemoteTerminate`, `runRemoteDiagnose`, `runRemoteNotify`,
  `runRemoteWithProgress`.
- `internal/psScript/psScript.go` — единый сценарий PowerShell. Его запускает
  локальный `powershell.exe` (`-Action find|findfast|terminate|notify|diagnose`,
  `-DryRun`), подключаясь к серверу по CIM/DCOM. Ответ — строки протокола
  `STAGE|`, `TIMING|`, `ERROR|`, `DIAG|`, `NOTIFY_*|`.
- Тестовые кнопки («Тест закрытия 1С», «Тестовое сообщение») идут тем же путём,
  что и боевые, и выходят перед необратимым шагом. Тесты в
  `internal/psScript/*_test.go` это проверяют.
- `internal/xlsx` — своя запись .xlsx без внешних зависимостей.
- `frontend/src` — интерфейс на TypeScript без фреймворка (Vite).

## Дома, вне рабочей сети

- Сервер обновлений `192.168.150.100:1213` недоступен — проверка обновлений
  просто не найдёт его, это не ошибка сборки.
- Список серверов можно загрузить из файла (`docs/servers.example.json`).
- Тесты `internal/psScript` запускают PowerShell/WMI на этом компьютере. Если
  они падают из-за окружения (нет прав, политика), остальное проверяется
  `go test ./internal/ops/... ./internal/xlsx/...` и т.д.

## Выпуск версии (на сервере 192.168.150.100)

```powershell
git fetch; git reset --hard origin/main
.\tools\release.ps1 -Version 1.0.11 -Notes "Что нового" -SkipTests
```

Подробнее про Nginx и CI — [DEPLOY-NGINX-CICD.md](DEPLOY-NGINX-CICD.md).
