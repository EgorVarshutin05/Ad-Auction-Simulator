package web

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPageIncludesAssets(t *testing.T) {
	handler, err := Handler()
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", "/", nil))
	if response.Code != 200 {
		t.Fatalf("status = %d", response.Code)
	}
	body := response.Body.String()
	for _, name := range []string{"style.css", "app.js"} {
		asset, err := Files.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(body, string(asset)) {
			t.Errorf("page does not contain unescaped %s", name)
		}
	}
	if strings.Contains(body, `href="/static/`) || strings.Contains(body, `src="/static/`) {
		t.Error("page still requires external assets")
	}
	if response.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Error("missing HTML content type")
	}
}
