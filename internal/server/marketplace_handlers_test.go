// Blocked: depends on gated marketplace_handlers.go catalog handlers.
//go:build opencode_wip

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opencode-go/opencode-go/internal/provider"
)

func newMarketplaceTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	workdir := t.TempDir()
	return New(Options{Provider: provider.NewMock("hi"), Model: "mock", Workdir: workdir}), workdir
}

func getMarketplaceList(t *testing.T, base, path string) marketplaceListResponse {
	t.Helper()
	resp, err := http.Get(base + path)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("%s status = %d", path, resp.StatusCode)
	}
	var got marketplaceListResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	return got
}

func TestTUIOpenThemesNonEmpty(t *testing.T) {
	srv, _ := newMarketplaceTestServer(t)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/tui/open-themes", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var got tuiOpenThemesResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if !got.Opened {
		t.Fatal("opened false")
	}
	if got.Current.ID == "" || got.Current.Name == "" || got.Current.Source == "" {
		t.Fatalf("current missing: %+v", got.Current)
	}
	if len(got.Items) == 0 {
		t.Fatal("themes empty")
	}
}

func TestMarketplacePluginsIncludeInstalledConfigPlugin(t *testing.T) {
	t.Setenv("OPENCODE_CONFIG_CONTENT", `{"plugin":["my-plugin"]}`)
	srv, _ := newMarketplaceTestServer(t)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	got := getMarketplaceList(t, ts.URL, "/marketplace/plugins")
	for _, item := range got.Items {
		if item.ID == "my-plugin" && item.Installed {
			return
		}
	}
	t.Fatalf("installed plugin missing: %+v", got.Items)
}

func TestMarketplacePluginsDoNotIncludeLSPFormatter(t *testing.T) {
	srv, _ := newMarketplaceTestServer(t)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	got := getMarketplaceList(t, ts.URL, "/marketplace/plugins")
	for _, item := range got.Items {
		if item.ID == "lsp" || item.ID == "formatter" {
			t.Fatalf("unexpected built-in plugin %s present", item.ID)
		}
	}
}

func TestThemeSelectInvalidID400(t *testing.T) {
	srv, _ := newMarketplaceTestServer(t)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/theme/select", "application/json", strings.NewReader(`{"id":"../dark"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestThemeSelectValidWritesState(t *testing.T) {
	srv, workdir := newMarketplaceTestServer(t)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/theme/select", "application/json", strings.NewReader(`{"id":"dark"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	data, err := os.ReadFile(filepath.Join(workdir, ".opencode", "theme.json"))
	if err != nil {
		t.Fatal(err)
	}
	var state struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	if state.ID != "dark" {
		t.Fatalf("theme id = %q", state.ID)
	}
	if mode := os.FileMode(0); true {
		info, err := os.Stat(filepath.Join(workdir, ".opencode", "theme.json"))
		if err != nil {
			t.Fatal(err)
		}
		mode = info.Mode().Perm()
		if mode != 0600 {
			t.Fatalf("mode = %v", mode)
		}
	}
}

func TestMalformedManifestWarnsNotCrash(t *testing.T) {
	srv, workdir := newMarketplaceTestServer(t)
	if err := os.MkdirAll(filepath.Join(workdir, ".opencode"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workdir, ".opencode", "marketplace.json"), []byte(`{"themes":`), 0600); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	got := getMarketplaceList(t, ts.URL, "/marketplace/themes")
	if len(got.Items) == 0 {
		t.Fatal("themes empty")
	}
	if got.Status != "warning" || len(got.Warnings) == 0 {
		t.Fatalf("warning missing: %+v", got)
	}
}

func TestManifestInvalidIDWarnsAndDrops(t *testing.T) {
	srv, workdir := newMarketplaceTestServer(t)
	if err := os.MkdirAll(filepath.Join(workdir, ".opencode"), 0700); err != nil {
		t.Fatal(err)
	}
	manifest := `{"themes":[{"id":"valid-theme","name":"Valid"},{"id":"../bad","name":"Bad"}],"plugins":[{"id":"valid-plugin"},{"id":"bad/plugin"}]}`
	if err := os.WriteFile(filepath.Join(workdir, ".opencode", "marketplace.json"), []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	themes := getMarketplaceList(t, ts.URL, "/marketplace/themes")
	if themes.Status != "warning" || len(themes.Warnings) != 1 || themes.Warnings[0] != "marketplace manifest contained invalid ids" {
		t.Fatalf("invalid id warning missing: %+v", themes)
	}
	if !catalogHasID(themes.Items, "valid-theme") || catalogHasID(themes.Items, "../bad") {
		t.Fatalf("theme catalog invalid: %+v", themes.Items)
	}
	plugins := getMarketplaceList(t, ts.URL, "/marketplace/plugins")
	if plugins.Status != "warning" || len(plugins.Warnings) != 1 || plugins.Warnings[0] != "marketplace manifest contained invalid ids" {
		t.Fatalf("invalid id warning missing: %+v", plugins)
	}
	if !catalogHasID(plugins.Items, "valid-plugin") || catalogHasID(plugins.Items, "bad/plugin") {
		t.Fatalf("plugin catalog invalid: %+v", plugins.Items)
	}
}

func TestOversizedManifestWarns(t *testing.T) {
	srv, workdir := newMarketplaceTestServer(t)
	if err := os.MkdirAll(filepath.Join(workdir, ".opencode"), 0700); err != nil {
		t.Fatal(err)
	}
	data := append([]byte(`{"themes":[]}`), make([]byte, maxMarketplaceBytes)...)
	if err := os.WriteFile(filepath.Join(workdir, ".opencode", "marketplace.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	got := getMarketplaceList(t, ts.URL, "/marketplace/themes")
	if got.Status != "warning" || len(got.Warnings) != 1 || got.Warnings[0] != "marketplace manifest too large" {
		t.Fatalf("too large warning missing: %+v", got)
	}
}
