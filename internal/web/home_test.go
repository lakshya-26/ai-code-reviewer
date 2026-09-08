package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHomeHandler_ServesLanding(t *testing.T) {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	HomeHandler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, `src="/logo.svg"`) {
		t.Fatal("missing DiffSense mascot")
	}
	if strings.Contains(body, `<span class="mark">DS</span>`) {
		t.Fatal("letter mark should be replaced by the logo")
	}
	if !strings.Contains(body, githubAppURL) {
		t.Fatal("missing GitHub App install URL")
	}
	if !strings.Contains(body, `href="/setup"`) {
		t.Fatal("missing setup page link")
	}
	if !strings.Contains(body, "API key") {
		t.Fatal("missing setup / API key explanation")
	}
	if !strings.Contains(body, "first pass") {
		t.Fatal("missing first-pass disclaimer")
	}
	if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("content-type %q", ct)
	}
}

func TestLogoHandler_ServesOwl(t *testing.T) {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/logo.svg", nil)
	LogoHandler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "<svg") || !strings.Contains(rr.Header().Get("Content-Type"), "image/svg+xml") {
		t.Fatal("expected SVG mascot")
	}
	if !strings.Contains(body, "#7CFFB2") {
		t.Fatal("missing brand fill")
	}
}

func TestSetupHandler_MissingIDShowsForm(t *testing.T) {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/setup", nil)
	SetupHandler(nil).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, `name="installation_id"`) {
		t.Fatal("expected installation ID form")
	}
}

func TestHomeHandler_UnknownPath404(t *testing.T) {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/not-a-page", nil)
	HomeHandler().ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status %d", rr.Code)
	}
}
