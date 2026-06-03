package main

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/labstack/echo/v5"
)

type failingWriter struct{}

func (failingWriter) Write(p []byte) (int, error) {
	return 0, errors.New("write failed")
}

func TestGetDataDir(t *testing.T) {
	t.Setenv("DIARUM_DATA_PATH", "")
	t.Setenv("DIARIA_DATA_PATH", "")
	if got := getDataDir(); got != "./diarum_data" {
		t.Fatalf("getDataDir default = %q, want ./diarum_data", got)
	}

	t.Setenv("DIARIA_DATA_PATH", "/legacy")
	if got := getDataDir(); got != "/legacy" {
		t.Fatalf("getDataDir legacy env = %q, want /legacy", got)
	}

	t.Setenv("DIARUM_DATA_PATH", "/preferred")
	if got := getDataDir(); got != "/preferred" {
		t.Fatalf("getDataDir preferred env = %q, want /preferred", got)
	}
}

func TestServeSPA(t *testing.T) {
	fsys := fstest.MapFS{
		"index.html":           &fstest.MapFile{Data: []byte("root-index")},
		"assets/app.js":        &fstest.MapFile{Data: []byte("console.log('ok')")},
		"nested/index.html":    &fstest.MapFile{Data: []byte("nested-index")},
		"nested/ignored.txt":   &fstest.MapFile{Data: []byte("ignored")},
		"not-a-dir/file.html":  &fstest.MapFile{Data: []byte("file-html")},
	}

	tests := []struct {
		name       string
		path       string
		wantStatus int
		wantBody   string
		wantErr    error
	}{
		{name: "api path", path: "/api/v1/test", wantErr: echo.ErrNotFound},
		{name: "exact file", path: "/assets/app.js", wantStatus: http.StatusOK, wantBody: "console.log('ok')"},
		{name: "directory index", path: "/nested/", wantStatus: http.StatusOK, wantBody: "nested-index"},
		{name: "fallback index", path: "/missing", wantStatus: http.StatusOK, wantBody: "root-index"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := echo.New()
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			err := serveSPA(c, fs.FS(fsys))
			if tt.wantErr != nil {
				if err != tt.wantErr {
					t.Fatalf("serveSPA error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("serveSPA: %v", err)
			}
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if body := rec.Body.String(); body != tt.wantBody {
				t.Fatalf("body = %q, want %q", body, tt.wantBody)
			}
		})
	}
}

func TestVersionGlobals(t *testing.T) {
	if Version == "" || Name == "" {
		t.Fatalf("Version/Name should not be empty: %q / %q", Version, Name)
	}
}

func TestRunVersionAndUnknownCommand(t *testing.T) {
	var out bytes.Buffer
	if err := run([]string{"version"}, &out); err != nil {
		t.Fatalf("run version: %v", err)
	}
	if !strings.Contains(out.String(), Version) {
		t.Fatalf("version output = %q", out.String())
	}

	if err := run([]string{"unknown"}, io.Discard); err == nil || !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("run unknown command error = %v", err)
	}
	if err := run([]string{"version"}, failingWriter{}); err == nil || err.Error() != "write failed" {
		t.Fatalf("run version writer error = %v, want write failed", err)
	}
}

func TestRunServe(t *testing.T) {
	originalStartServer := startServer
	defer func() { startServer = originalStartServer }()

	var capturedAddr string
	startServer = func(e *echo.Echo, addr string) error {
		capturedAddr = addr
		if len(e.Router().Routes()) == 0 {
			t.Fatal("server should register routes before starting")
		}
		return http.ErrServerClosed
	}

	if err := run([]string{"serve", "-data-dir", t.TempDir(), "-http", ":9191"}, io.Discard); err != nil {
		t.Fatalf("run serve: %v", err)
	}
	if capturedAddr != ":9191" {
		t.Fatalf("capturedAddr = %q, want :9191", capturedAddr)
	}

	startServer = func(e *echo.Echo, addr string) error {
		return errors.New("boom")
	}
	if err := run([]string{"serve", "-data-dir", t.TempDir(), "-http", ":9292"}, io.Discard); err == nil || err.Error() != "boom" {
		t.Fatalf("run serve error = %v, want boom", err)
	}

	filePath := filepath.Join(t.TempDir(), "file-data-dir")
	if err := os.WriteFile(filePath, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("WriteFile data-dir file: %v", err)
	}
	if err := run([]string{"serve", "-data-dir", filePath}, io.Discard); err == nil {
		t.Fatal("run serve should fail when data-dir points to a file")
	}
}
