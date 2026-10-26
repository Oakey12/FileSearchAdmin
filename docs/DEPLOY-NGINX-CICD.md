# FileSearchAdmin — развёртывание через Nginx и CI/CD

Документ: анализ проекта, что уже известно про сервер Nginx, как настроить
раздачу приложения и автоматический выпуск (CI/CD).

---

## 1. Анализ проекта и работоспособность

**Что это.** Десктоп-утилита администрирования 1С (Windows) — полный порт
`1Cproc` на **Wails v2.16** (бэкенд Go, окно WebView2, интерфейс на ванильном
TypeScript в стиле FileSearch). Удалённые операции идут через локальный
`powershell.exe` → CIM/DCOM к целевым серверам (оповещения `msg.exe`, поиск и
завершение `1cv7s`, команды PowerShell, сканер программ/файлов, сведения о
системе).

**Состояние на сейчас (ветка `main`, коммит `c3167c4`):**

| Проверка | Команда | Результат |
|---|---|---|
| Статический анализ | `go vet ./...` | чисто |
| Go-тесты | `go test ./...` | все проходят |
| Тесты интерфейса | `cd frontend && npx vitest run` | 35 тестов проходят |
| Типы TS | `cd frontend && npx tsc --noEmit` | чисто |
| Сборка | `wails build -s -skipbindings` | `build\bin\filesearch-admin.exe` (~12 МБ) |
| Запуск | двойной клик по exe | окно открывается, журнал пишется |

Вывод: **проект работоспособен и собирается.** Готовый бинарник лежит в
`build\bin\filesearch-admin.exe` (в репозиторий не коммитится — см. `.gitignore`)
и скопирован в `G:\ИТ отдел\Утилиты\FileSearchAdmin\filesearch-admin.exe`.

**Важно про сборку:** `frontend/dist` и `frontend/wailsjs` лежат в репозитории,
поэтому серверу выпуска **не нужен Node.js** — только Go 1.25+ и Wails CLI.
Node нужен только при правке интерфейса (`frontend/src`).

**Куда приложение ходит за обновлениями и списком серверов** (уже прописано в коде):
- список серверов: `http://192.168.150.100:1212/filesearch-admin/servers.json`
  (переопределяется переменной среды `FSADMIN_SERVERS_URL`);
- обновления: `http://192.168.150.100:1212/filesearch-admin/latest.json`.

То есть код **уже нацелен** на ваш Nginx-сервер — остаётся создать там каталог
`filesearch-admin` и выложить файлы.

---

## 2. Где работает Nginx и что на нём сейчас крутится

Проверено по сети с машины разработки (AI2, 192.168.100.16). Сервер Nginx —
**192.168.150.100**:

| Порт | Служба | Что отдаёт |
|---|---|---|
| **1212** | nginx/1.28.3 | Обновления **FileSearch**: `/file-search/latest.json` → 200. Корень `/` → 404 (автолистинг выключен). |
| **8080** | nginx/1.28.3 | Страница **1Cproc** («Удалённое управление 1cv7s»). |
| **80** | Microsoft-IIS/10.0 | Отдельный сайт на IIS (не Nginx). |
| **443** | (IIS/HTTPS) | HTTPS с самоподписанным сертификатом. |

Путь `/filesearch-admin/` на `:1212` сейчас свободен (404) — его и займём.

### Как подтвердить это на самом сервере (192.168.150.100)

Зайдите на сервер (RDP) и выполните в PowerShell:

```powershell
# 1. Процессы и путь к nginx.exe
Get-Process nginx | Select-Object Id, Path

# 2. Какие порты слушает nginx (сопоставьте PID с шагом 1)
Get-NetTCPConnection -State Listen | Sort-Object LocalPort |
  Select-Object LocalPort, OwningProcess

# 3. Где конфиг и что в нём за сайты (server/listen/root/location)
#    Путь к nginx возьмите из шага 1, например C:\Users\1108\nginx-1.28.3
$nginx = 'C:\Users\1108\nginx-1.28.3'
Get-Content "$nginx\conf\nginx.conf"
Select-String -Path "$nginx\conf\*.conf","$nginx\conf\**\*.conf" `
  -Pattern 'listen|server_name|root|location'
```

В `nginx.conf` вы увидите блок `server { listen 1212; ... root <путь>; }` —
это и есть сервер обновлений. Запомните его `root` (ниже он нужен).

---

## 3. Настройка Nginx под FileSearchAdmin

Рекомендуемый вариант — **переиспользовать существующий сервер `:1212`** и
просто добавить подкаталог `filesearch-admin`. Он требует минимум (часто —
нуля) изменений конфига, потому что `:1212` уже раздаёт любые подкаталоги
своего `root` (так работает `/file-search/`).

### Шаг 1. Определите `root` сервера `:1212`

Из `nginx.conf` (см. раздел 2). По аналогии с FileSearch это, скорее всего:

```
C:\Users\1108\nginx-1.28.3\html-filesearch
```

тогда `/file-search/` = `...\html-filesearch\file-search\`, а новый
`/filesearch-admin/` = `...\html-filesearch\filesearch-admin\`.

### Шаг 2. Создайте каталог приложения и выложите список серверов

```powershell
$root = 'C:\Users\1108\nginx-1.28.3\html-filesearch'   # root сервера :1212
New-Item -ItemType Directory -Force "$root\filesearch-admin" | Out-Null
# Положите список серверов 1С (формат — см. docs/servers.example.json)
Copy-Item .\docs\servers.example.json "$root\filesearch-admin\servers.json"
```

Отредактируйте `servers.json` под свои серверы (имя + IP, адреса уникальны).
Формат:

```json
{
  "servers": [
    { "name": "Сервер 1С (основной)", "ip": "192.168.20.31" },
    { "name": "Терминальный сервер",  "ip": "192.168.20.40" }
  ]
}
```

### Шаг 3. Проверьте

```powershell
# правильный MIME для .json (обычно уже есть в mime.types)
Invoke-WebRequest http://192.168.150.100:1212/filesearch-admin/servers.json -UseBasicParsing
```

Если `servers.json` отдаётся с кодом 200 — настройка Nginx закончена.
**Отдельный `server`-блок или правку `nginx.conf` добавлять не нужно**, если
`:1212` уже раздаёт подкаталоги своего `root`.

> Если на `:1212` подкаталоги **не** раздаются (у сервера строгий `location`),
> добавьте в его `server { listen 1212; }` блок и перечитайте конфиг
> (`nginx -s reload`):
>
> ```nginx
> location /filesearch-admin/ {
>     root   html-filesearch;   # тот же root, что и у /file-search/
>     types { application/json json; application/octet-stream exe; }
>     default_type application/octet-stream;
> }
> ```

### (Необязательно) Страница скачивания

Если нужна веб-страница «скачать утилиту» (как у 1Cproc на `:8080`), положите
`index.html` в `...\filesearch-admin\` со ссылкой на `FileSearchAdmin.zip`
(этот архив кладёт `release.ps1`, см. ниже).

### Брандмауэр

Порт `1212` уже открыт (FileSearch работает), отдельных правил не требуется.

---

## 4. Выпуск версии вручную (без CI)

На сервере (или на машине с Go+Wails), из корня репозитория:

```powershell
powershell -ExecutionPolicy Bypass -File .\tools\release.ps1 -Version 1.0.0 -Notes "Первый релиз"
```

Скрипт `tools/release.ps1`:
1. прогоняет `go test ./...`;
2. собирает `filesearch-admin-<версия>.exe` через `wails build` с версией в
   `-ldflags`;
3. кладёт в `...\filesearch-admin\`: `filesearch-admin-<версия>.exe`,
   `latest.json` (манифест с SHA-256), `FileSearchAdmin.zip`.

После этого кнопка «Обновить» в приложении увидит новую версию.
`servers.json` скрипт **не трогает** — он ведётся отдельно (раздел 3, шаг 2).

---

## 5. CI/CD — автоматический выпуск

Репозиторий на GitHub (`github.com/Oakey12/FileSearchAdmin`). Nginx-сервер во
внутренней сети, облачные раннеры GitHub до него не достанут, поэтому
используем **self-hosted раннер на самом Nginx-сервере**: он собирает и сразу
кладёт файлы в локальный каталог Nginx. Workflow уже в репозитории:
`.github/workflows/release.yml`.

### Шаг 1. Подготовить сервер (однократно)

На 192.168.150.100 должны быть в `PATH`:
- **Git**, **Go 1.25+**, **Wails CLI**
  (`go install github.com/wailsapp/wails/v2/cmd/wails@latest` → добавить
  `%USERPROFILE%\go\bin` в PATH). Node.js не нужен.

Проверка: `git --version; go version; wails version`.

### Шаг 2. Установить GitHub Actions self-hosted runner

GitHub → репозиторий → **Settings → Actions → Runners → New self-hosted runner
→ Windows x64** и выполнить показанные команды на сервере. При настройке задать
метки **`self-hosted,windows`**. Раннер лучше поставить **службой**
(`svc.cmd install && svc.cmd start`), чтобы работал без входа в систему.

Раннеру нужен только исходящий HTTPS к GitHub (вход извне не требуется).

### Шаг 3. Выпуск

Теперь релиз — это тег:

```bash
git tag v1.0.1
git push origin v1.0.1
```

Workflow сам: сделает checkout тега → `tools/release.ps1 -Version 1.0.1
-SkipGitChecks` → соберёт и опубликует в каталог Nginx. Также можно запустить
вручную: **Actions → Release FileSearchAdmin → Run workflow** и ввести версию.

> `-SkipGitChecks` отключает проверки чистоты дерева/отставания от `origin/main`
> — в CI раннер и так делает чистый checkout нужного коммита.

### Проверка каталога публикации

По умолчанию `release.ps1` публикует в
`C:\Users\1108\nginx-1.28.3\html-filesearch\filesearch-admin`. Если `root`
сервера `:1212` другой — передайте его параметром:

```powershell
.\tools\release.ps1 -Version 1.0.1 -PublishDir 'D:\nginx\html\filesearch-admin' -SkipGitChecks
```

(в workflow это можно добавить в строку запуска `release.ps1`).

### Альтернатива без раннера

Если ставить раннер нельзя — запланированная задача на сервере: раз в N минут
`git fetch`, и если появился новый тег `v*` — запустить `release.ps1`. Это
проще, но без обратной связи и логов GitHub Actions.

---

## 6. Чеклист запуска «с нуля»

1. [ ] На сервере подтвердить `root` сервера `:1212` (раздел 2).
2. [ ] Создать `...\filesearch-admin\`, положить и отредактировать `servers.json` (раздел 3).
3. [ ] Проверить `http://192.168.150.100:1212/filesearch-admin/servers.json` → 200.
4. [ ] Поставить на сервер Go + Wails (CI) или собрать вручную (раздел 4).
5. [ ] (CI) Установить self-hosted runner, выпустить тег `v1.0.0`.
6. [ ] Раздать `filesearch-admin.exe` (или `FileSearchAdmin.zip`) сотрудникам.
7. [ ] Проверить кнопку «Обновить» в приложении на следующем релизе.
