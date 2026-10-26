package constandstruct

import "time"

const (
	AppVersion = "6.6.0"

	CREATE_NO_WINDOW = 0x08000000

	TargetProcessNames = "1cv7s.exe"

	ServerTimeout         = 2 * time.Minute
	MutationServerTimeout = 2 * time.Minute
	UninstallTimeout      = 15 * time.Minute
	FileOperationTimeout  = 2 * time.Minute
	RemoteUserScanTimeout = 5 * time.Minute
	ProgramScanTimeout    = 5 * time.Minute
	OperationTimeout      = 10 * time.Minute

	MaxParallelServers = 2

	// Проверка программы по списку компьютеров только читает реестр и диск,
	// поэтому её можно вести шире обычных операций: список бывает в сотни ПК.
	MaxParallelAppChecks = 8
)

// Итог проверки одного компьютера в ApplicationCheck.Status.
const (
	AppCheckInstalled = "installed"
	AppCheckMissing   = "missing"
	AppCheckError     = "error"
)

type EmployeeProcess struct {
	ServerName string
	ServerIP   string
	Owner      string
	PIDs       []int
	Sessions   []int
}

type Server struct {
	Name string `json:"name"`
	IP   string `json:"ip"`
}

type InstalledProgram struct {
	Name                 string `json:"Name"`
	Version              string `json:"Version"`
	Publisher            string `json:"Publisher"`
	InstallDate          string `json:"InstallDate"`
	Architecture         string `json:"Architecture"`
	Owner                string `json:"Owner"`
	RegistryPath         string `json:"RegistryPath"`
	CanSilentUninstall   bool   `json:"CanSilentUninstall"`
	UninstallDescription string `json:"UninstallDescription"`
}

type RemoteFile struct {
	Name             string `json:"Name"`
	FullPath         string `json:"FullPath"`
	Extension        string `json:"Extension"`
	Size             int64  `json:"Size"`
	LastWriteTimeUTC string `json:"LastWriteTimeUTC"`
	IsText           bool   `json:"IsText"`
}

type RemoteUser struct {
	DisplayName string `json:"DisplayName"`
	Username    string `json:"Username"`
	Domain      string `json:"Domain"`
	SID         string `json:"SID"`
	ProfilePath string `json:"ProfilePath"`
	SessionID   uint32 `json:"SessionID"`
}

// ApplicationMatch — найденное приложение. Source говорит, чем именно оно
// обнаружено: записью в реестре, запущенным процессом или файлом на диске.
type ApplicationMatch struct {
	Source  string `json:"Source"`
	Owner   string `json:"Owner"`
	Name    string `json:"Name"`
	Version string `json:"Version"`
	Path    string `json:"Path"`
}

// ApplicationCheck — ответ одного компьютера при проверке программы по списку.
// Summary — главная найденная запись, Owners — у кого она стоит, Warnings —
// чего сценарий не смог увидеть (например, профили без активного сеанса).
type ApplicationCheck struct {
	Name     string             `json:"name"`
	IP       string             `json:"ip"`
	Status   string             `json:"status"`
	Summary  string             `json:"summary"`
	Owners   []string           `json:"owners"`
	Matches  []ApplicationMatch `json:"matches"`
	Warnings []string           `json:"warnings"`
	Error    string             `json:"error"`
}

// SystemInfo — сводка о целевом компьютере для системного администратора.
// Числовые поля памяти и частоты приходят строками: их форматирует удалённый
// скрипт, чтобы разделитель дробной части не зависел от локали машины.
type SystemInfo struct {
	ComputerName      string   `json:"ComputerName"`
	Domain            string   `json:"Domain"`
	PartOfDomain      bool     `json:"PartOfDomain"`
	LoggedOnUser      string   `json:"LoggedOnUser"`
	Manufacturer      string   `json:"Manufacturer"`
	Model             string   `json:"Model"`
	SerialNumber      string   `json:"SerialNumber"`
	OSCaption         string   `json:"OSCaption"`
	OSVersion         string   `json:"OSVersion"`
	OSBuild           string   `json:"OSBuild"`
	OSArchitecture    string   `json:"OSArchitecture"`
	OSInstallDate     string   `json:"OSInstallDate"`
	LastBootTime      string   `json:"LastBootTime"`
	Uptime            string   `json:"Uptime"`
	ProcessorName     string   `json:"ProcessorName"`
	ProcessorCores    int      `json:"ProcessorCores"`
	ProcessorLogical  int      `json:"ProcessorLogical"`
	ProcessorClockMHz int      `json:"ProcessorClockMHz"`
	MemoryTotalGB     string   `json:"MemoryTotalGB"`
	MemoryFreeGB      string   `json:"MemoryFreeGB"`
	IPAddresses       []string `json:"IPAddresses"`
	Disks             []string `json:"Disks"`
	UserFolders       []string `json:"UserFolders"`
}

type RemoteFolderResult struct {
	Files     []RemoteFile
	Truncated bool
}

type RemoteTextDocument struct {
	File           RemoteFile
	Content        string
	Encoding       string
	OriginalSHA256 string
}

type RemoteSaveResult struct {
	File       RemoteFile
	BackupPath string
}

type RemoteWordProcess struct {
	PID     int
	Session int
	Name    string
	Owner   string
}

var ServerList []Server
