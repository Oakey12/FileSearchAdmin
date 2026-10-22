package remotefunc

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

var netBIOSFileServerPattern = regexp.MustCompile(
	`(?mi)^\s*([A-Z0-9][A-Z0-9_-]{0,14})\s+<20>\s+`,
)

const targetComputerNameCacheTTL = 10 * time.Minute

type targetComputerNameCacheEntry struct {
	name      string
	expiresAt time.Time
}

var targetComputerNameCache = struct {
	sync.Mutex
	entries map[string]targetComputerNameCacheEntry
}{entries: make(map[string]targetComputerNameCacheEntry)}

// resolveRemoteUsername принимает привычный локальный формат .\user. Если имя
// целевого компьютера удаётся определить по IP, используется COMPUTER\user;
// иначе в WMI передаётся неподготовленное имя user, которое также является
// допустимым форматом локальной учётной записи для удалённого WMI-подключения.
func resolveRemoteUsername(ctx context.Context, target, username string) (string, error) {
	username = strings.TrimSpace(username)
	if !strings.HasPrefix(username, `.\`) {
		return username, nil
	}

	account := strings.TrimSpace(strings.TrimPrefix(username, `.\`))
	if account == "" || strings.ContainsAny(account, `\/@`) {
		return "", fmt.Errorf("после .\\ должно быть указано имя локального пользователя")
	}

	computerName, err := resolveTargetComputerName(ctx, target)
	if err != nil {
		return account, nil
	}
	return computerName + `\` + account, nil
}

func resolveTargetComputerName(ctx context.Context, target string) (string, error) {
	target = strings.TrimSpace(target)
	if net.ParseIP(target) == nil {
		if name := shortWindowsComputerName(target); name != "" {
			return name, nil
		}
		return "", fmt.Errorf("некорректное имя целевого компьютера")
	}
	if name, ok := cachedTargetComputerName(target, time.Now()); ok {
		return name, nil
	}

	lookupCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	results := make(chan string, 2)

	go func() {
		names, err := net.DefaultResolver.LookupAddr(lookupCtx, target)
		if err == nil {
			for _, name := range names {
				if shortName := shortWindowsComputerName(name); shortName != "" {
					results <- shortName
					return
				}
			}
		}
		results <- ""
	}()

	go func() {
		command := exec.CommandContext(lookupCtx, "nbtstat.exe", "-A", target)
		command.SysProcAttr = hideWindowAttr()
		output, err := command.CombinedOutput()
		if err == nil {
			results <- computerNameFromNBTStat(output)
			return
		}
		results <- ""
	}()

	for range 2 {
		select {
		case name := <-results:
			if name != "" {
				cacheTargetComputerName(target, name, time.Now())
				return name, nil
			}
		case <-lookupCtx.Done():
			return "", lookupCtx.Err()
		}
	}
	return "", fmt.Errorf("обратная DNS-запись и имя NetBIOS не найдены")
}

func cachedTargetComputerName(target string, now time.Time) (string, bool) {
	targetComputerNameCache.Lock()
	defer targetComputerNameCache.Unlock()
	entry, ok := targetComputerNameCache.entries[target]
	if !ok {
		return "", false
	}
	if !now.Before(entry.expiresAt) {
		delete(targetComputerNameCache.entries, target)
		return "", false
	}
	return entry.name, true
}

func cacheTargetComputerName(target, name string, now time.Time) {
	targetComputerNameCache.Lock()
	targetComputerNameCache.entries[target] = targetComputerNameCacheEntry{
		name:      name,
		expiresAt: now.Add(targetComputerNameCacheTTL),
	}
	targetComputerNameCache.Unlock()
}

func computerNameFromNBTStat(output []byte) string {
	match := netBIOSFileServerPattern.FindSubmatch(output)
	if len(match) != 2 {
		return ""
	}
	return strings.ToUpper(string(match[1]))
}

func shortWindowsComputerName(value string) string {
	value = strings.TrimSpace(strings.TrimSuffix(value, "."))
	if value == "" || net.ParseIP(value) != nil {
		return ""
	}
	name := strings.SplitN(value, ".", 2)[0]
	if len(name) == 0 || len(name) > 15 {
		return ""
	}
	for _, character := range name {
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') ||
			character == '-' || character == '_' {
			continue
		}
		return ""
	}
	return strings.ToUpper(name)
}
