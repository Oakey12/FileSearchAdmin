# FileSearchAdmin — развёртывание через Nginx (порт 1213) и CI/CD

Приложение раздаётся с **отдельного порта Nginx — `1213`** (у FileSearch —
`1212`, не пересекается). Документ: анализ проекта, что известно про сервер
Nginx, пошаговая настройка порта 1213 и автоматический выпуск (CI/CD).

---

## 1. Анализ проекта и работоспособность

Десктоп-утилита администрирования 1С (Windows) — порт `1Cproc` на **Wails
v2.16** (Go + WebView2, интерфейс на TypeScript). Удалённые операции идут через
локальный `powershell.exe` → CIM/DCOM к целевым серверам.

Состояние (`main`): `go vet` чист, Go-тесты и 35 vitest зелёные, `tsc` чист,
`wails build -s -skipbindings` даёт `build\bin\filesearch-admin.exe` (~12 МБ),
окно запускается. **Проект работоспособен.**

Готовый exe скопирован в `G:\ИТ отдел\Утилиты\FileSearchAdmin\filesearch-admin.exe`.

**Адреса, зашитые в приложение (уже на порт 1213):**
- список серверов: `http://192.168.150.100:1213/servers.json`
  (переопределяется переменной среды `FSADMIN_SERVERS_URL`);
- обновления: `http://192.168.150.100:1213/latest.json`.

Сборке не нужен Node.js — `frontend/dist` и `frontend/wailsjs` лежат в
репозитории. Нужны только **Go 1.25+** и **Wails CLI**.

---

## 2. Сервер Nginx: где работает и что уже крутится

Проверено по сети. Сервер — **192.168.150.100**:

| Порт | Служба | Назначение |
|---|---|---|
| **1212** | nginx/1.28.3 | Обновления FileSearch (`/file-search/*`). |
| **8080** | nginx/1.28.3 | Страница 1Cproc. |
| **80** | Microsoft-IIS/10.0 | Отдельный сайт на IIS. |
| **443** | IIS/HTTPS | HTTPS (самоподписанный сертификат). |
| **1213** | — | **Свободен** (закрыт). Его и настроим под FileSearchAdmin. |

### Подтвердить на самом сервере (RDP на 192.168.150.100, PowerShell)

```powershell
# 1) Процессы nginx и путь к nginx.exe
Get-Process nginx | Select-Object Id, Path

# 2) Что nginx слушает сейчас
Get-NetTCPConnection -State Listen | Sort-Object LocalPort |
  Select-Object LocalPort, OwningProcess

# 3) Конфиг и путь установки nginx (prefix). Путь — из шага 1,
#    например C:\Users\1108\nginx-1.28.3
$nginx = 'C:\Users\1108\nginx-1.28.3'
Get-Content "$nginx\conf\nginx.conf"
```

Запомните **путь установки nginx** (prefix) — в нём лежат `conf\nginx.conf`,
`nginx.exe` и каталоги сайтов (`html-...`). Ниже он обозначен `<NGINX>`.

---

## 3. Настройка порта 1213 под FileSearchAdmin

> **Быстрый путь.** Скопируйте на сервер `tools\nginx-setup.ps1` и запустите от
> администратора. Он сам найдёт `nginx.conf`, покажет текущие сайты, создаст
> каталог + `servers.json` и блок `conf\filesearch-admin.conf`, сделает
> резервную копию `nginx.conf` и напечатает **один** шаг, который делаете вы.
> `nginx.conf` скрипт не меняет — старые сайты не затрагиваются. Ниже — то же
> вручную.

Четыре шага: каталог → блок в конфиге → перезагрузка nginx → брандмауэр.

### Шаг 1. Каталог с файлами

```powershell
$nginx = 'C:\Users\1108\nginx-1.28.3'               # <NGINX> — путь установки
New-Item -ItemType Directory -Force "$nginx\html-filesearch-admin" | Out-Null
# Список серверов 1С (формат — docs\servers.example.json)
Copy-Item .\docs\servers.example.json "$nginx\html-filesearch-admin\servers.json"
```

Отредактируйте `servers.json` под свои серверы (имя + IP, адреса уникальны):

```json
{
  "servers": [
    { "name": "Сервер 1С (основной)", "ip": "192.168.20.31" },
    { "name": "Терминальный сервер",  "ip": "192.168.20.40" }
  ]
}
```

### Шаг 2. Блок server на порт 1213 в `nginx.conf`

Откройте `<NGINX>\conf\nginx.conf` и **внутри блока `http { … }`** (рядом с
существующим `server { listen 1212; … }`) добавьте:

```nginx
# FileSearchAdmin — обновления и список серверов
server {
    listen       1213;
    server_name  _;
    charset      utf-8;

    # путь относителен папки установки nginx (<NGINX>); можно и абсолютный
    root         html-filesearch-admin;

    location / {
        autoindex off;
        try_files  $uri =404;
    }
}
```

`.json` отдаётся как `application/json` (есть в `mime.types`), `.exe` — как
`application/octet-stream` (скачивается). Отдельная настройка MIME не нужна.

### Шаг 3. Проверить конфиг и перезагрузить nginx

```powershell
cd 'C:\Users\1108\nginx-1.28.3'      # <NGINX>
.\nginx.exe -t                        # проверка синтаксиса -> "syntax is ok"
.\nginx.exe -s reload                 # применить без остановки
```

### Шаг 4. Открыть порт 1213 в брандмауэре

```powershell
New-NetFirewallRule -DisplayName "Nginx FileSearchAdmin 1213" `
  -Direction Inbound -Protocol TCP -LocalPort 1213 -Action Allow
```

### Проверка

```powershell
Invoke-WebRequest http://192.168.150.100:1213/servers.json -UseBasicParsing
```

Код **200** и ваш JSON — настройка Nginx закончена. (`latest.json` появится
после первого релиза, раздел 4.)

### (Необязательно) Страница скачивания

Положите `index.html` в `html-filesearch-admin\` со ссылкой на
`FileSearchAdmin.zip` — тогда `http://192.168.150.100:1213/` будет страницей
«скачать утилиту» (архив кладёт `release.ps1`).

---

## 4. Выпуск версии вручную

На сервере (или на машине с Go + Wails), из корня репозитория:

```powershell
powershell -ExecutionPolicy Bypass -File .\tools\release.ps1 -Version 1.0.0 -Notes "Первый релиз"
```

Скрипт: прогоняет `go test ./...`, собирает `filesearch-admin-<версия>.exe`
через `wails build` (версия в `-ldflags`), и кладёт в `html-filesearch-admin\`:
`filesearch-admin-<версия>.exe`, `latest.json` (манифест с SHA-256),
`FileSearchAdmin.zip`. После этого кнопка «Обновить» в приложении увидит
новую версию. `servers.json` скрипт не трогает — он ведётся отдельно (раздел 3).

По умолчанию публикует в `C:\Users\1108\nginx-1.28.3\html-filesearch-admin`.
Если путь установки nginx другой — укажите явно:

```powershell
.\tools\release.ps1 -Version 1.0.0 -PublishDir '<NGINX>\html-filesearch-admin'
```

---

## 5. CI/CD — автоматический выпуск

Репозиторий на GitHub (`github.com/Oakey12/FileSearchAdmin`). Nginx-сервер во
внутренней сети, поэтому используем **self-hosted раннер на самом
Nginx-сервере**: он собирает и сразу кладёт файлы в локальный каталог.
Workflow в репозитории — `.github/workflows/release.yml`.

### Шаг 1. Подготовить сервер (однократно)

В `PATH` на 192.168.150.100 должны быть **git**, **go 1.25+**, **wails**
(`go install github.com/wailsapp/wails/v2/cmd/wails@latest` → добавить
`%USERPROFILE%\go\bin`). Node.js не нужен. Проверка:
`git --version; go version; wails version`.

### Шаг 2. Установить self-hosted runner

GitHub → репозиторий → **Settings → Actions → Runners → New self-hosted runner
→ Windows x64**, выполнить показанные команды на сервере, метки
**`self-hosted,windows`**. Поставить службой (`svc.cmd install && svc.cmd
start`), чтобы работал без входа в систему. Раннеру нужен только исходящий
HTTPS к GitHub.

### Шаг 3. Выпуск — через тег

```bash
git tag v1.0.1
git push origin v1.0.1
```

Workflow сам: checkout тега → `tools\release.ps1 -Version 1.0.1
-SkipGitChecks` → сборка и публикация в каталог Nginx. Либо вручную:
**Actions → Release FileSearchAdmin → Run workflow** с вводом версии.

### Альтернатива без раннера

Запланированная задача на сервере: раз в N минут `git fetch`; если появился
новый тег `v*` — запустить `release.ps1`. Проще, но без логов Actions.

---

## 6. Чеклист запуска «с нуля»

1. [ ] На сервере: создать `<NGINX>\html-filesearch-admin`, положить и
       отредактировать `servers.json` (раздел 3, шаг 1).
2. [ ] Добавить `server { listen 1213; … }` в `nginx.conf` (шаг 2).
3. [ ] `nginx -t` → `nginx -s reload` (шаг 3).
4. [ ] Открыть TCP 1213 в брандмауэре (шаг 4).
5. [ ] Проверить `http://192.168.150.100:1213/servers.json` → 200.
6. [ ] Поставить Go + Wails на сервер (CI) или собрать вручную (раздел 4).
7. [ ] Выпустить версию: `release.ps1` вручную или тег `v1.0.0` (CI).
8. [ ] Раздать `filesearch-admin.exe` сотрудникам; проверить «Обновить».
