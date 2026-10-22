# FileSearchAdmin Port Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Port the entire admin functionality of `C:\Users\1108\FS\1Cproc` (Go+Fyne) into a new Wails v2.16 desktop app in `C:\Users\1108\FS\FileSearchAdmin`, styled like `C:\Users\1108\FS\FileSearch`.

**Architecture:** The remote transport in 1Cproc is UI-agnostic (it shells out to local `powershell.exe`, passes creds+script via stdin, PS opens a CIM/DCOM session to the target and runs `Win32_Process.Create`/WMI). The Node-independent Go packages port almost verbatim (new module path `filesearch-admin`). The Fyne UI and the Fyne-coupled parts of `buttonFunc` are rewritten onto a single bound `App` whose methods stream progress via `runtime.EventsEmit`, consumed by a vanilla-TS frontend that reuses FileSearch's visual infrastructure.

**Tech Stack:** Go 1.25+, Wails v2.16, vanilla TypeScript + Vite, Windows PowerShell 5.1 / CIM / DCOM. Node.js required only for Phase B (build frontend + generate wailsjs bindings).

**Spec:** `docs/superpowers/specs/2026-10-22-filesearchadmin-design.md`

## Global Constraints

- Module path: `filesearch-admin`. Windows-only (`//go:build windows` where source files were).
- Password never written to disk, journal, or URL; passed to PowerShell via stdin only (copy 1Cproc's `encodePasswordForStdin` + stdin flow verbatim).
- App manifest `asInvoker` (no elevation) — copy 1Cproc `app.manifest`.
- Node absent during Phase A: every Phase A task must pass with `go test ./...` and `go build` only. `wails build` / `wails dev` are Phase B.
- Release build stays `wails build -s -skipbindings`; `frontend/dist` and `frontend/wailsjs` are committed (Phase B).
- Reuse FileSearch frontend files **verbatim** where the design says drop-in: `style.css`, `theme.ts`, `backdrop.ts`, `toast.ts`, `format.ts`, `highlight.ts`.
- All user-facing strings Russian, matching 1Cproc wording.
- One operation at a time (port 1Cproc `beginOperation`/`finishOperation`/cancel); `MaxParallelServers = 2`.

## Review Focus

- **Password leakage into events/journal:** a `journal:line` event or log file must never contain the password or the raw stdin payload. Test in Task 6: feed a fake command runner, assert emitted lines + log file exclude the secret.
- **Frontend string rendering (XSS):** server names, journal lines, scan results, remote file names reach the DOM — must use `textContent`/safe nodes, never `innerHTML` of file-derived data. Test in Phase B tab tasks.
- **Operation re-entrancy:** starting a second operation while one runs must be rejected cleanly, and cancel must not leave `operationRunning=true`. Test in Task 5 (port 1Cproc `operation_test.go`).
- **Servers-list fallback:** when the servers URL is unreachable, the cached copy must load; when neither exists, manual IP must still work. Test in Task 4 (port 1Cproc `config/servers_remote_test.go`).
- **Credential parsing (`DOMAIN\user` / `.\user`):** malformed login must produce a clear journal error, not a panic or a silent empty user. Test in Task 6 (port 1Cproc `remoteFunc/credentials_test.go`).

---

## PHASE A — Go backend (no Node required)

### Task 1: Wails Go scaffold + module

**Files:**
- Create: `go.mod` (`module filesearch-admin`, `go 1.25`), `main.go`, `app.go`, `wails.json`, `app.manifest`, `build/windows/wails.exe.manifest`, `build/windows/info.json`, `build/appicon.png`, `.gitignore`, `.gitattributes`.
- Reference: FileSearch `main.go`, `wails.json`, `build/*` as templates.

**Interfaces:**
- Produces: `App` struct with `ctx context.Context`, `startup(ctx)`, `Version() string`; `NewApp() *App`; bound via `Bind: []any{app}` in `main.go`. Window title "Удалённое управление 1С — Администрирование", dark/light theme wiring copied from FileSearch `main.go`.
- Produces: a **placeholder** `frontend/dist/index.html` (static "загрузка…" page) and empty `frontend/dist/assets/` so `//go:embed all:frontend/dist` compiles in Phase A. Real dist comes in Phase B.

- [ ] Step 1: Write `go.mod` with module `filesearch-admin`, require wails v2.16.0 + deps copied from 1Cproc/FileSearch (`excelize`, `golang.org/x/sys`, `golang.org/x/text`).
- [ ] Step 2: Write `main.go` from FileSearch template: `//go:embed all:frontend/dist`, theme-from-file, `wails.Run` with title above, `Bind: []any{NewApp()}`. Remove FileSearch-specific options (DnD optional).
- [ ] Step 3: Write minimal `app.go`: `App` struct, `NewApp`, `startup`, `Version` returning a version const.
- [ ] Step 4: Create placeholder `frontend/dist/index.html`.
- [ ] Step 5: Run `go build ./...` — Expected: compiles.
- [ ] Step 6: Commit `feat: wails scaffold for filesearch-admin`.

### Task 2: Port leaf transport packages (model, psscript, office, journal-core)

**Files:**
- Create: `internal/model/` (← 1Cproc `internal/constAndStruct/*.go`), `internal/psscript/` (← `internal/psScript/*.go`), `internal/office/` (← `internal/officeEditor/*.go`), `internal/journal/` (← `internal/auxiliaryFunc/logger.go` + `journal.go` **minus** the Fyne widget — keep rotation/write core only).
- Test: port the matching `*_test.go` from each source package.

**Interfaces:**
- Produces: `model.Server`, `model.EmployeeProcess`, `model.SystemInfo`, `model.InstalledProgram`, `model.RemoteFile`, `model.RemoteUser`, etc. (rename package `constandstruct` → `model`); `model.MessageTemplates`, `model.AppVersion`, timeout consts.
- Produces: `psscript.PsScript`, `psscript.CommandScript`, `psscript.CleanupCommandScript`, `psscript.SystemInfoScript`, `psscript.InstalledProgramsScript`, `psscript.RemoteUsersScript`, `psscript.FindApplicationScript` (unchanged string bodies).
- Produces: `office` docx/xlsx editors (unchanged); `journal.Logger` with rotation (no Fyne).

- [ ] Step 1: Copy source files, rewrite package names + import paths to `filesearch-admin/internal/...`. For `journal`, drop the `fyne.Do` scheduling and the widget; keep the logger's file rotation and the plain journal buffer with a `func(string)` sink.
- [ ] Step 2: Copy the corresponding `*_test.go`; adjust imports.
- [ ] Step 3: Run `go test ./internal/model/... ./internal/psscript/... ./internal/office/... ./internal/journal/...` — Expected: PASS.
- [ ] Step 4: Commit `feat: port model/psscript/office/journal packages`.

### Task 3: Port remote transport (`internal/remote`)

**Files:**
- Create: `internal/remote/` (← 1Cproc `internal/remoteFunc/*.go`: `remote.go`, `credentials.go`, `share_windows.go`).
- Test: port `remote_test.go`, `credentials_test.go`, `cleanup_test.go`, `share_windows_test.go`, `remote_integration_test.go`.

**Interfaces:**
- Consumes: `internal/psscript`, `internal/model`.
- Produces: `remote.RunRemoteWithProgress(ctx, ip, user, password, action, arg, payload string, progress ProgressFunc) ([]string, error)`, `remote.RunRemoteScript(...)`, cleanup helpers, `remote.ProgressFunc`, credential parse/format helpers. Signatures copied verbatim from 1Cproc.

- [ ] Step 1: Copy files, rewrite imports to `filesearch-admin/internal/{psscript,model}`.
- [ ] Step 2: Copy tests; adjust imports.
- [ ] Step 3: Run `go test ./internal/remote/...` — Expected: PASS (the password-via-stdin flow and CP866 decoding are covered by ported tests).
- [ ] Step 4: Commit `feat: port remote CIM/DCOM transport`.

### Task 4: Port servers config (`internal/config`)

**Files:**
- Create: `internal/config/servers.go` (← 1Cproc `internal/config/servers.go`).
- Test: port `servers_test.go`, `servers_remote_test.go`.

**Interfaces:**
- Consumes: `internal/model`.
- Produces: `config.Load(ctx) ([]model.Server, error)` with the URL/cache/fallback logic; env overrides renamed `FSADMIN_SERVERS_URL` / `FSADMIN_SERVERS_FILE`; default URL const (placeholder `http://192.168.150.100:1212/filesearch-admin/servers.json`, final value set at the Nginx step). Cache dir `%LOCALAPPDATA%\FileSearchAdmin`.

- [ ] Step 1: Copy `servers.go`, rename env vars + default URL + cache dir, rewrite imports.
- [ ] Step 2: Copy both tests; update env-var names and expected URL/cache path.
- [ ] Step 3: Run `go test ./internal/config/...` — Expected: PASS, incl. unreachable-URL → cache fallback and no-cache → manual path.
- [ ] Step 4: Commit `feat: port servers-list config with cache fallback`.

### Task 5: Operation state on App (`internal/ops` + App operation layer)

**Files:**
- Create: `internal/ops/` for the **Fyne-free** orchestration ported from 1Cproc `buttonFunc` (scanner.go, systemInfo.go, findApplication.go, remote_files.go, remote_office.go, remote_users.go, uninstall.go, command.go core) — the parts that take plain args and return values.
- Modify: `app.go` — add operation state (`operationMu sync.Mutex`, `operationRunning bool`, `operationCancel context.CancelFunc`, `operationWG sync.WaitGroup`) ported from `buttonFunc/button.go` globals; `beginOperation`/`finishOperation`/`CancelOperation`.
- Test: `app_operation_test.go` (← 1Cproc `buttonFunc/operation_test.go`), `internal/ops/*_test.go` (port the Fyne-free `*_test.go`: `scanner_test.go`, `systemInfo_test.go`, `findApplication_test.go`, `remote_files_test.go`, `remote_office_test.go`, `remote_users_test.go`, `uninstall_test.go`, `command_test.go`).

**Interfaces:**
- Consumes: `internal/remote`, `internal/model`, `internal/office`.
- Produces: pure functions e.g. `ops.ScanSystemInfo(ctx, ip, user, pass string, progress remote.ProgressFunc) (model.SystemInfo, error)`, `ops.ScanPrograms(...)`, `ops.ScanFiles(...)`, `ops.ScanUsers(...)`, `ops.SearchProcesses(...)`, `ops.Notify(...)`, `ops.RunCommand(...)` — each returns data + error instead of mutating Fyne widgets and calling `logLine`.
- Produces on App: `(a *App) beginOperation() (context.Context, bool)`, `(a *App) finishOperation()`, `(a *App) CancelOperation()`, `(a *App) emitLine(string)` (wraps `runtime.EventsEmit(ctx,"journal:line",...)` + journal.Logger).

- [ ] Step 1: Write `app_operation_test.go`: assert second `beginOperation` returns `false` while one runs; `CancelOperation` resets `operationRunning` to false after `finishOperation`.
- [ ] Step 2: Run it — Expected: FAIL (methods undefined).
- [ ] Step 3: Port the operation globals from `button.go` into App methods; port Fyne-free ops functions into `internal/ops` (replace `logLine func(string)` params with a `progress remote.ProgressFunc`, replace widget mutation with return values).
- [ ] Step 4: Run `go test ./internal/ops/... . -run Operation` — Expected: PASS.
- [ ] Step 5: Commit `feat: operation state + Fyne-free ops layer`.

### Task 6: App methods + event contract (the rewrite of `button.go`/`command.go`)

**Files:**
- Modify: `app.go` — add the bound methods that the frontend calls; each parses inputs, calls `ops.*` in a goroutine under `beginOperation`, streams `journal:line`, emits `operation:status`.
- Test: `app_methods_test.go` — table tests with a **fake transport** seam (inject a `runner` interface so no real DCOM needed), asserting: `Notify` emits a `journal:line` without the password; `GetSelectedServers`/credential parsing errors surface as journal lines not panics.

**Interfaces:**
- Consumes: `internal/ops`, `internal/config`, `internal/model`, `internal/journal`.
- Produces (bound, called from frontend): `ServerList() []model.Server`, `Notify(targets []model.Server, user, password, message string)`, `SearchProcesses(targets, user, password)`, `ScanEmployees(...)`, `CompleteEmployees(...)`, `FinishAll(...)`, `RunCommand(ip, user, password, script string, session *uint32)`, `ScanUsers(ip, user, password) ([]model.RemoteUser, error)`, `ScanPrograms(ip, user, password) ([]model.InstalledProgram, error)`, `UninstallProgram(...)`, `ScanFiles(...)`, `ReadRemoteFile(...)`, `SaveRemoteFile(...)`, `DeleteRemoteFile(...)`, `ScanSystemInfo(ip, user, password) (model.SystemInfo, error)`, `PickScriptFile() (string, error)` (Wails `OpenFileDialog`), `CancelOperation()`.
- Produces (events): `journal:line` (string), `operation:status` (`{running bool, label string}`), `scan:progress` (optional). Document the exact JSON shape in a comment block in `app.go`.
- Produces: a `runner` interface in `internal/ops` whose production impl calls `remote`, overridable in tests.

- [ ] Step 1: Introduce the `runner` seam in `internal/ops`; write `app_methods_test.go` asserting password absence in emitted lines (capture events via a test sink) and clean error on bad login.
- [ ] Step 2: Run — Expected: FAIL.
- [ ] Step 3: Implement the bound methods (rewrite of `button.go`/`command.go` logic: select targets, read creds, run under `beginOperation`, stream lines). Dialogs via Wails runtime.
- [ ] Step 4: Run `go test ./... ` — Expected: PASS, `go vet ./...` clean.
- [ ] Step 5: Commit `feat: App methods + journal/operation event contract`.

### Task 7: Update + release (Node-independent parts)

**Files:**
- Create: `internal/update/` — port 1Cproc `internal/updater` (helper self-replace via `--pid/--current/--new`) **or** adopt FileSearch `internal/update` in-process rename. Decision: **FileSearch in-process** (simpler, proven in sibling) unless a locked-exe case needs the helper.
- Create: `tools/release.ps1` (← FileSearch `tools/release.ps1`, retargeted: exe name `filesearch-admin-<ver>.exe`, `$PublishDir` under `C:\Users\1108\nginx-1.28.3\html-filesearch\filesearch-admin`, manifest URL to match).
- Modify: `main.go`/`app.go` — `startup` calls `update.CleanupOld()` + `go a.watchUpdates()`; add `CheckUpdate`/`InstallUpdate` bound methods + `update:available`/`update:progress` events (port from FileSearch `app.go`).
- Test: port FileSearch `internal/update/update_test.go` (version compare, manifest parse, sha256).

**Interfaces:**
- Produces: `update.CurrentVersion`, `update.ManifestURL` (ldflags-injected), `update.CheckLatest(ctx)`, `update.Download`, `update.Apply`, `update.CleanupOld`.

- [ ] Step 1: Port `internal/update` from FileSearch; rename module path + default manifest URL (placeholder).
- [ ] Step 2: Port `update_test.go`.
- [ ] Step 3: Run `go test ./internal/update/...` — Expected: PASS.
- [ ] Step 4: Add `CheckUpdate`/`InstallUpdate`/`watchUpdates` to App; write `tools/release.ps1`.
- [ ] Step 5: Run `go build ./...` + `go test ./...` — Expected: PASS.
- [ ] Step 6: Commit `feat: self-update + release pipeline`.

**End of Phase A: `go build ./...` produces a runnable binary with a placeholder window; `go test ./...` green; `go vet` clean. The whole backend is exercised headless.**

---

## PHASE B — Frontend + window (requires Node installed)

### Task 8: Frontend toolchain + reused infrastructure

**Files:**
- Create: `frontend/package.json`, `frontend/tsconfig.json`, `frontend/index.html`, `frontend/vite-env.d.ts` (← FileSearch templates).
- Copy verbatim from FileSearch `frontend/src/`: `style.css`, `theme.ts`, `backdrop.ts`, `toast.ts`, `format.ts`, `highlight.ts` (+ their `*.test.ts`).
- Create: `frontend/src/go.ts` — a thin facade re-exporting the needed `wailsjs/go/main/App` methods + `EventsOn` channels (the design's "Go-facade" to avoid scattered imports).

**Interfaces:**
- Produces: `frontend/src/go.ts` exporting typed wrappers for every bound method + `onJournalLine(cb)`, `onOperationStatus(cb)`, `onUpdateAvailable(cb)`.

- [ ] Step 1: `cd frontend && npm install` (needs Node). Expected: lockfile resolves.
- [ ] Step 2: Copy reused modules + tests; `npx wails generate module` (or `wails generate`) to create `frontend/wailsjs` bindings from the Go App.
- [ ] Step 3: Write `go.ts` facade over the generated bindings.
- [ ] Step 4: Run `npx vitest run` — Expected: reused suites (theme/backdrop/highlight) PASS.
- [ ] Step 5: Commit `feat: frontend toolchain + reused FileSearch infra`.

### Task 9: App shell — tabs, layout, journal drawer, themes/backgrounds

**Files:**
- Create: `frontend/src/main.ts` (bootstrap: theme/backdrop before paint, mount tabs, wire journal events), `frontend/src/tabs.ts`, `frontend/src/journal.ts` (drawer consuming `journal:line` + copy + open-log-folder), `frontend/src/creds.ts` (server multiselect + IP + login + password card, shared across tabs).
- Modify: `frontend/index.html` (backdrop scene scaffold from FileSearch, tab roots, toast host).

**Interfaces:**
- Consumes: `go.ts`, reused `theme.ts`/`backdrop.ts`/`toast.ts`.
- Produces: a `CredState` object (selected servers, ip, login, password) read by each tab; `Journal` with `line(s)`, `clear()`, status chip.

- [ ] Step 1: Build the shell: three tabs ("Завершение 1С", "Команды", "Сканер и удаление"), journal drawer, creds card. Render all file-derived strings via `textContent`.
- [ ] Step 2: `npx tsc --noEmit` — Expected: no type errors.
- [ ] Step 3: Commit `feat: app shell, tabs, journal drawer, creds card`.

### Task 10: Tab "Завершение 1С" + notifications

**Files:** Create `frontend/src/tab-main.ts`.
**Interfaces:** Consumes `go.ts` (`ServerList`, `SearchProcesses`, `ScanEmployees`, `CompleteEmployees`, `FinishAll`, `Notify`), `CredState`, `Journal`.
- [ ] Step 1: Build controls: search 1cv7s, scan employees (checkbox list in dialog), complete-for-selected, finish-all, notify row (template `<select>` from `model.MessageTemplates` + text + "Оповестить").
- [ ] Step 2: `npx tsc --noEmit` — Expected: clean. Commit.

### Task 11: Tab "Команды"

**Files:** Create `frontend/src/tab-commands.ts`.
**Interfaces:** Consumes `go.ts` (`RunCommand`, `ScanUsers`, `PickScriptFile`), `CredState`, `Journal`.
- [ ] Step 1: Mode switch (machine / user-session), user/session picker (via `ScanUsers`), script textarea + attach `.ps1` (via `PickScriptFile`), execute with confirm dialog. Output to journal.
- [ ] Step 2: `npx tsc --noEmit` — clean. Commit.

### Task 12: Tab "Сканер и удаление" + editors

**Files:** Create `frontend/src/tab-scanner.ts`, `frontend/src/file-editor.ts` (text/docx/xlsx editing panes), `frontend/src/sysinfo.ts`.
**Interfaces:** Consumes `go.ts` (`ScanPrograms`, `UninstallProgram`, `ScanFiles`, `ReadRemoteFile`, `SaveRemoteFile`, `DeleteRemoteFile`, `ScanSystemInfo`, `ScanUsers`), `CredState`, `Journal`.
- [ ] Step 1: Programs sub-tab (scan → list → uninstall with confirm); Files sub-tab (by path / by user session → list → open editor → save/delete with confirm + backup note); System-info sub-tab (collect + copy).
- [ ] Step 2: `npx tsc --noEmit` — clean. Commit.

### Task 13: Build, wire update UI, run

**Files:** Modify `main.ts` (update-available chip + install sheet, like FileSearch); build outputs.
- [ ] Step 1: Add update UI consuming `update:available`/`update:progress`, calling `CheckUpdate`/`InstallUpdate`.
- [ ] Step 2: `cd frontend && npm run build` → produces `frontend/dist`. Commit `dist` + `wailsjs`.
- [ ] Step 3: `wails build -s -skipbindings` — Expected: single exe builds.
- [ ] Step 4: Launch the exe; smoke-check: window opens in the chosen theme, three tabs render, backgrounds switch, journal drawer opens, a scan with a fake/invalid IP produces a clean journal error (no crash). Real end-to-end over DCOM is run by the user on their network.
- [ ] Step 5: Commit `feat: update UI + committed dist/wailsjs + release build`.

---

## PHASE C — Nginx (separate, after the app is working)

Retarget `tools/release.ps1` `$PublishDir`/`ManifestURL` to the real Nginx path+port (sibling of FileSearch's `html-filesearch\file-search`, e.g. `html-filesearch\filesearch-admin` on port 1212), publish `servers.json` + `latest.json`, and wire the final `config` default URL + `update.ManifestURL` ldflags. Done as its own step with the user.
