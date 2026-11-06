package app

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"herd/internal/store"
)

func TestDiagrams(t *testing.T) {
	a, _ := newTestApp(t)
	tv, _ := a.CreateTask("core", "", "draw it", "", false, nil)
	_ = a.db.SaveStep(store.Step{ID: store.StepID(tv.ID, "impl-2"), TaskID: tv.ID, Key: "impl-2", Agent: "impl", N: 2, Seq: 1, State: store.Done})
	os.MkdirAll(tv.RunDir, 0o755)
	write := func(name, body string) { os.WriteFile(filepath.Join(tv.RunDir, name), []byte(body), 0o644) }
	write("IMPLEMENT-2-flow.html", "<html><head><title>New lock flow</title></head></html>")
	write("other.html", "<html></html>")
	write("PLAN.md", "not a page")

	ds, err := a.Diagrams(tv.ID)
	if err != nil || len(ds) != 2 {
		t.Fatalf("diagrams: %+v %v", ds, err)
	}
	if ds[0].File != "IMPLEMENT-2-flow.html" || ds[0].Title != "New lock flow" || ds[0].Step != "Implement 2" || ds[1].Step != "" {
		t.Fatalf("diagrams: %+v", ds)
	}

	get := func(file string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		a.RunFiles().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/run/"+url.PathEscape(tv.ID)+"/"+file, nil))
		return w
	}
	w := get("IMPLEMENT-2-flow.html")
	if w.Code != 200 || !strings.Contains(w.Header().Get("Content-Security-Policy"), "sandbox allow-scripts") || !strings.Contains(w.Body.String(), "New lock flow") {
		t.Fatalf("served: %d %q", w.Code, w.Header())
	}
	if got := string(withTheme([]byte("<!doctype html><html lang=en><body>"), "dark")); got != `<!doctype html><html data-theme="dark" lang=en><body>` {
		t.Fatal(got)
	}
	if got := string(withTheme([]byte(`<html data-theme="light">`), "dark")); got != `<html data-theme="light">` {
		t.Fatal(got)
	}
	for _, bad := range []string{"PLAN.md", "..%2F..%2Fsecret.html", ".hidden.html", "missing.html"} {
		if w := get(bad); w.Code != 404 {
			t.Errorf("%s: %d", bad, w.Code)
		}
	}
}
