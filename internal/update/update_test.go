package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.2.0", "1.1.9", 1},
		{"v1.2", "1.2.0", 0},
		{"1.10.0", "1.9.0", 1},
		{"0.9", "1.0", -1},
		{"1.0.0", "dev", 1},
		{"1.0.0-rc1", "1.0.0", 0},
	}
	for _, c := range cases {
		if got := compareVersions(c.a, c.b); got != c.want {
			t.Errorf("compareVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func withServer(t *testing.T, manifest string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/file-search/latest.json":
			w.Write([]byte(manifest))
		case "/file-search/file-search-2.0.0.exe":
			w.Write([]byte("fake exe"))
		default:
			http.NotFound(w, r)
		}
	}))
	oldURL, oldVer := ManifestURL, CurrentVersion
	t.Cleanup(func() {
		srv.Close()
		ManifestURL, CurrentVersion = oldURL, oldVer
	})
	ManifestURL = srv.URL + "/file-search/latest.json"
	CurrentVersion = "1.0.0"
	return srv
}

func TestCheckLatest(t *testing.T) {
	sum := sha256.Sum256([]byte("fake exe"))
	srv := withServer(t, "\xef\xbb\xbf"+`{"version":"2.0.0","url":"file-search-2.0.0.exe","sha256":"`+
		hex.EncodeToString(sum[:])+`","notes":"тест"}`)

	rel, newer, err := CheckLatest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !newer {
		t.Error("ожидалось, что 2.0.0 новее 1.0.0")
	}
	if want := srv.URL + "/file-search/file-search-2.0.0.exe"; rel.URL != want {
		t.Errorf("URL = %q, want %q", rel.URL, want)
	}
}

func TestCheckLatestNotFound(t *testing.T) {
	srv := withServer(t, "{}")
	ManifestURL = srv.URL + "/missing.json"
	if _, _, err := CheckLatest(context.Background()); !errors.Is(err, ErrNoReleases) {
		t.Fatalf("err = %v, want ErrNoReleases", err)
	}
}

func TestDownloadChecksumMismatch(t *testing.T) {
	srv := withServer(t, "{}")
	rel := Release{
		Version: "2.0.0",
		URL:     srv.URL + "/file-search/file-search-2.0.0.exe",
		SHA256:  hex.EncodeToString(make([]byte, 32)),
	}
	_, err := Download(context.Background(), rel, nil)
	if !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("err = %v, want ErrChecksumMismatch", err)
	}
	exe, _ := executablePath()
	if _, err := os.Stat(exe + ".new"); !os.IsNotExist(err) {
		t.Error("файл .new должен быть удалён после ошибки")
	}
}