package app

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWebHandlerServesStaticMultiPageApplication(t *testing.T) {
	webDir := t.TempDir()
	writeWebFixture(t, webDir, "index.html", "documents page")
	writeWebFixture(t, webDir, "history/index.html", "history page")
	writeWebFixture(t, webDir, "assets/app.js", "application script")
	writeWebFixture(t, webDir, "404.html", "missing page")

	server := &Server{cfg: Config{WebDir: webDir}}
	tests := []struct {
		name       string
		path       string
		wantStatus int
		wantBody   string
	}{
		{name: "root page", path: "/", wantStatus: http.StatusOK, wantBody: "documents page"},
		{name: "directory without slash", path: "/history?repo=7", wantStatus: http.StatusOK, wantBody: "history page"},
		{name: "directory with slash", path: "/history/?repo=7", wantStatus: http.StatusOK, wantBody: "history page"},
		{name: "static asset", path: "/assets/app.js", wantStatus: http.StatusOK, wantBody: "application script"},
		{name: "unknown page", path: "/does-not-exist/", wantStatus: http.StatusNotFound, wantBody: "missing page"},
		{name: "cleaned traversal stays in web root", path: "/../outside.txt", wantStatus: http.StatusNotFound, wantBody: "missing page"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			server.webHandler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, tt.path, nil))
			if recorder.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, tt.wantStatus)
			}
			if !strings.Contains(recorder.Body.String(), tt.wantBody) {
				t.Fatalf("body = %q, want it to contain %q", recorder.Body.String(), tt.wantBody)
			}
		})
	}
}

func TestRoutesKeepAPIPrefixOutOfStaticPages(t *testing.T) {
	webDir := t.TempDir()
	writeWebFixture(t, webDir, "index.html", "documents page")
	writeWebFixture(t, webDir, "404.html", "missing page")
	server := &Server{cfg: Config{WebDir: webDir}}

	for _, requestPath := range []string{"/api", "/api/unknown"} {
		t.Run(requestPath, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			server.Routes().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, requestPath, nil))

			if recorder.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
			}
			if strings.Contains(recorder.Body.String(), "documents page") || strings.Contains(recorder.Body.String(), "missing page") {
				t.Fatalf("API response unexpectedly used a static page: %q", recorder.Body.String())
			}
		})
	}
}

func writeWebFixture(t *testing.T, root, name, contents string) {
	t.Helper()
	fullPath := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatalf("create fixture directory: %v", err)
	}
	if err := os.WriteFile(fullPath, []byte(contents), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}
