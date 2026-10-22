// Package update проверяет и устанавливает обновления с собственного сервера Nginx.
package update

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Задаются при сборке:
//
//	-ldflags "-X filesearch-admin/internal/update.CurrentVersion=1.2.0"
//	-ldflags "-X filesearch-admin/internal/update.ManifestURL=http://host:1212/filesearch-admin/latest.json"
var (
	CurrentVersion = "1.0.0"
	ManifestURL    = "http://192.168.150.100:1212/filesearch-admin/latest.json"
)

var (
	ErrNoReleases       = errors.New("на сервере нет опубликованной версии")
	ErrUnavailable      = errors.New("сервер обновлений недоступен")
	ErrChecksumMismatch = errors.New("контрольная сумма скачанного файла не совпадает")
)

// Release описывает latest.json на сервере.
type Release struct {
	Version string `json:"version"`
	URL     string `json:"url"`
	SHA256  string `json:"sha256"`
	Size    int64  `json:"size,omitempty"`
	Notes   string `json:"notes,omitempty"`
}

const maxManifestSize = 64 << 10

var (
	manifestClient = &http.Client{Timeout: 15 * time.Second}
	downloadClient = &http.Client{} // время ограничивается контекстом
)

// CheckLatest читает манифест. Второе значение равно true, если на сервере версия новее текущей.
func CheckLatest(ctx context.Context) (Release, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ManifestURL, nil)
	if err != nil {
		return Release{}, false, err
	}
	req.Header.Set("Cache-Control", "no-cache")

	resp, err := manifestClient.Do(req)
	if err != nil {
		return Release{}, false, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return Release{}, false, ErrNoReleases
	case resp.StatusCode != http.StatusOK:
		return Release{}, false, fmt.Errorf("сервер обновлений ответил %s", resp.Status)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxManifestSize))
	if err != nil {
		return Release{}, false, fmt.Errorf("чтение манифеста: %w", err)
	}
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")) // BOM из PowerShell/Блокнота

	var rel Release
	if err := json.Unmarshal(data, &rel); err != nil {
		return Release{}, false, fmt.Errorf("неверный формат манифеста: %w", err)
	}
	if err := rel.normalize(ManifestURL); err != nil {
		return Release{}, false, err
	}
	return rel, compareVersions(rel.Version, CurrentVersion) > 0, nil
}

func (r *Release) normalize(base string) error {
	r.Version = strings.TrimSpace(r.Version)
	r.SHA256 = strings.ToLower(strings.TrimSpace(r.SHA256))
	r.URL = strings.TrimSpace(r.URL)

	if _, ok := parseVersion(r.Version); !ok {
		return fmt.Errorf("в манифесте неверная версия %q", r.Version)
	}
	if len(r.SHA256) != sha256.Size*2 {
		return errors.New("в манифесте нет корректного sha256")
	}
	if _, err := hex.DecodeString(r.SHA256); err != nil {
		return errors.New("в манифесте нет корректного sha256")
	}
	if r.URL == "" {
		return errors.New("в манифесте не указан url")
	}

	baseURL, err := url.Parse(base)
	if err != nil {
		return fmt.Errorf("неверный адрес манифеста: %w", err)
	}
	ref, err := url.Parse(r.URL)
	if err != nil {
		return fmt.Errorf("неверный url в манифесте: %w", err)
	}
	r.URL = baseURL.ResolveReference(ref).String()
	return nil
}

// Download скачивает новую версию в <exe>.new рядом с программой и проверяет sha256.
// progress может быть nil; total равен -1, если размер неизвестен.
func Download(ctx context.Context, rel Release, progress func(done, total int64)) (string, error) {
	exe, err := executablePath()
	if err != nil {
		return "", err
	}
	target := exe + ".new"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rel.URL, nil)
	if err != nil {
		return "", err
	}
	resp, err := downloadClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("скачивание: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("скачивание: сервер ответил %s", resp.Status)
	}

	total := resp.ContentLength
	if total <= 0 && rel.Size > 0 {
		total = rel.Size
	}

	f, err := os.Create(target)
	if err != nil {
		return "", fmt.Errorf("нет прав на запись в папку программы: %w", err)
	}
	ok := false
	defer func() {
		if !ok {
			f.Close()
			os.Remove(target)
		}
	}()

	h := sha256.New()
	pw := &progressWriter{total: total, fn: progress}
	if _, err := io.Copy(io.MultiWriter(f, h, pw), resp.Body); err != nil {
		return "", fmt.Errorf("скачивание: %w", err)
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != rel.SHA256 {
		return "", fmt.Errorf("%w: ожидалось %s, получено %s", ErrChecksumMismatch, rel.SHA256, got)
	}

	ok = true
	return target, nil
}

// Apply подменяет текущий exe скачанным и запускает новую версию.
// После успешного вызова приложение нужно закрыть.
func Apply(newPath string) error {
	exe, err := executablePath()
	if err != nil {
		return err
	}
	old := exe + ".old"
	_ = os.Remove(old)

	// Запущенный exe в Windows нельзя перезаписать, но можно переименовать.
	if err := os.Rename(exe, old); err != nil {
		return fmt.Errorf("не удалось переименовать текущий exe: %w", err)
	}
	if err := os.Rename(newPath, exe); err != nil {
		_ = os.Rename(old, exe)
		return fmt.Errorf("не удалось установить новую версию: %w", err)
	}

	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.Dir = filepath.Dir(exe)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("новая версия установлена, но не запустилась: %w", err)
	}
	return cmd.Process.Release()
}

// CleanupOld удаляет остатки прошлого обновления (.old и .new). Вызывать при старте.
func CleanupOld() {
	exe, err := executablePath()
	if err != nil {
		return
	}
	go func() {
		_ = os.Remove(exe + ".new")
		// Старый процесс может ещё завершаться, поэтому несколько попыток.
		for i := 0; i < 20; i++ {
			err := os.Remove(exe + ".old")
			if err == nil || errors.Is(err, os.ErrNotExist) {
				return
			}
			time.Sleep(500 * time.Millisecond)
		}
	}()
}

func executablePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("не удалось определить путь к программе: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return exe, nil
}

type progressWriter struct {
	done, total int64
	fn          func(done, total int64)
	last        time.Time
}

func (p *progressWriter) Write(b []byte) (int, error) {
	p.done += int64(len(b))
	if p.fn != nil && (time.Since(p.last) >= 100*time.Millisecond || p.done == p.total) {
		p.last = time.Now()
		p.fn(p.done, p.total)
	}
	return len(b), nil
}

// parseVersion разбирает "1.2.3", "v1.2" и "1.2.3-rc1" (суффикс отбрасывается).
func parseVersion(s string) ([3]int, bool) {
	var v [3]int
	s = strings.TrimLeft(strings.TrimSpace(s), "vV")
	if i := strings.IndexAny(s, "-+ "); i >= 0 {
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	if len(parts) > 3 {
		return v, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return v, false
		}
		v[i] = n
	}
	return v, true
}

// compareVersions возвращает 1, если a > b, -1, если a < b, и 0, если равны.
// Неразбираемая версия (например, "dev") считается 0.0.0.
func compareVersions(a, b string) int {
	va, _ := parseVersion(a)
	vb, _ := parseVersion(b)
	for i := range va {
		switch {
		case va[i] > vb[i]:
			return 1
		case va[i] < vb[i]:
			return -1
		}
	}
	return 0
}
