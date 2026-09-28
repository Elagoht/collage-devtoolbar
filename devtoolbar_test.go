package devtoolbar_test

import (
	"context"
	"errors"
	"html"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	devtoolbar "github.com/Elagoht/collage-devtoolbar"
	"github.com/Elagoht/collage/pkg/collage"
)

// checker reports one warning and one error on every render, as a checking plugin
// registered before the toolbar would.
type checker struct{}

func (checker) Name() string                             { return "test/checker" }
func (checker) Version() string                          { return "0" }
func (checker) Init(context.Context, collage.Host) error { return nil }
func (checker) Shutdown(context.Context) error           { return nil }
func (checker) OnAfterRender(_ context.Context, ev *collage.AfterRenderEvent) error {
	ev.Warn("w", "a warning")
	ev.Error("e", "an error")
	return nil
}

const page = `<!doctype html><html><head><title>t</title></head><body><main>hello</main></body></html>`

func app(t *testing.T, dev bool, plugins ...collage.Plugin) *collage.App {
	t.Helper()
	a, err := collage.New(&collage.Config{
		DevMode: dev,
		Server:  collage.ServerConfig{Host: "localhost", Port: 3000},
		Template: collage.TemplateConfig{FS: fstest.MapFS{
			"t/p.html":     {Data: []byte(page)},
			"t/shell.html": {Data: []byte(`<!doctype html><html><body>{{slot "side"}}{{slot "main"}}</body></html>`)},
			"t/part.html":  {Data: []byte(`<p>part</p>`)},
		}, Root: "t"},
		Plugins: plugins,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.RegisterPage(collage.NewPage("home").WithContent(collage.NewFragment("home", "p.html").Build()).WithPath("en", "/").Static().Build()); err != nil {
		t.Fatal(err)
	}
	if err := a.RegisterPage(collage.NewPage("x<y").WithContent(collage.NewFragment("xy", "p.html").Build()).WithPath("en", "/odd").Build()); err != nil {
		t.Fatal(err)
	}
	side := collage.NewFragment("side", "part.html").WithDataHandler(
		func(context.Context, *collage.RenderContext) (any, []string, error) { // any: DataHandlerFunc's own signature
			return nil, nil, errors.New("backend down")
		}).WithFallback(collage.NewFragment("side-fallback", "part.html").Build()).Build()
	main := collage.NewFragment("main", "part.html").WithDataHandler(
		func(context.Context, *collage.RenderContext) (any, []string, error) { // any: DataHandlerFunc's own signature
			return nil, []string{"posts"}, nil
		}).Build()
	shell := collage.NewFragment("shell", "shell.html").
		WithSlot("side", false, false).WithSlotFragment("side", side).
		WithSlot("main", false, false).WithSlotFragment("main", main).Build()
	if err := a.RegisterPage(collage.NewPage("mixed").WithContent(shell).WithPath("en", "/mixed").WithDependency("site").Build()); err != nil {
		t.Fatal(err)
	}
	if err := a.RegisterDocument(collage.NewDocument("data", "application/json").AtRoot("/data.json").WithBody([]byte(`{"a":1}`)).Build()); err != nil {
		t.Fatal(err)
	}
	if err := a.Handle("/raw/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: 1\n\n"))
		if http.NewResponseController(w).Flush() != nil {
			w.Header().Set("X-Flush", "failed")
		}
	})); err != nil {
		t.Fatal(err)
	}
	if err := a.Handle("/undeclared", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("<!doctype html><html><body><p>İstanbul</p>"))
		_, _ = w.Write([]byte("</body></html>"))
	})); err != nil {
		t.Fatal(err)
	}
	return a
}

func do(h http.Handler, method, path string, header ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, nil)
	for i := 0; i+1 < len(header); i += 2 {
		r.Header.Set(header[i], header[i+1])
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestPanelInDevelopment(t *testing.T) {
	h := app(t, true, checker{}, devtoolbar.New()).Handler()
	w := do(h, http.MethodGet, "/")
	body := w.Body.String()
	at := strings.Index(body, `<div id="collage-devtoolbar"`)
	if w.Code != http.StatusOK || at < 0 || at > strings.LastIndex(body, "</body>") {
		t.Fatalf("no panel before </body>:\n%s", body)
	}
	panel := body[at:strings.LastIndex(body, "</body>")]
	for _, want := range []string{
		"home (en)",
		"<dd>static</dd>",
		"<dd>200 OK</dd>",
		"<dd>" + w.Header().Get("X-Collage-Render-Time") + "</dd>",
		"<dd>" + html.EscapeString(w.Header().Get("ETag")) + "</dd>",
		"<dd>1 errors, 1 warnings</dd>",
		"2 findings",
		`aria-label="Close the development toolbar"`,
		"<details>",
	} {
		if !strings.Contains(panel, want) {
			t.Errorf("panel lacks %q:\n%s", want, panel)
		}
	}
	if w.Header().Get("X-Collage-Render-Time") == "" {
		t.Error("collage set no render time in development")
	}
	if got := w.Header().Get("Content-Length"); got != strconv.Itoa(len(body)) {
		t.Errorf("Content-Length = %s, body is %d", got, len(body))
	}
	if strings.Contains(panel, "<script") || strings.Contains(panel, "http://") || strings.Contains(panel, "https://") {
		t.Error("the panel loads something")
	}
}

// Each fragment's time and failure, and the tags the render depended on.
func TestFragmentsAndTags(t *testing.T) {
	h := app(t, true, devtoolbar.New()).Handler()
	body := do(h, http.MethodGet, "/mixed").Body.String()
	at := strings.Index(body, `<div id="collage-devtoolbar"`)
	if at < 0 {
		t.Fatalf("no panel:\n%s", body)
	}
	panel := body[at:]
	for _, want := range []string{
		"1 failed fragment",
		"<dt>Fragments</dt><dd>3 fragments, 1 failed</dd>",
		`<dt class="cdt-sub">shell</dt>`,
		`<dt class="cdt-sub">main</dt>`,
		`<dt class="cdt-sub cdt-failed">side</dt>`,
		"failed, fallback shown: ",
		"backend down",
		"<dt>Dependency tags</dt><dd>posts, site</dd>",
		"<dt>Degraded</dt>",
	} {
		if !strings.Contains(panel, want) {
			t.Errorf("panel lacks %q:\n%s", want, panel)
		}
	}
	// A page with no tags says so, and no fragment failed.
	home := do(h, http.MethodGet, "/").Body.String()
	if !strings.Contains(home, "<dt>Dependency tags</dt><dd>none</dd>") || !strings.Contains(home, "<dd>1 fragment</dd>") || strings.Contains(home, `class="cdt-sub cdt-failed"`) || strings.Contains(home, "failed fragment") {
		t.Errorf("home panel:\n%s", home[strings.Index(home, `<div id="collage-devtoolbar"`):])
	}
}

// Values in the panel are text, however a page is named.
func TestPanelEscapes(t *testing.T) {
	h := app(t, true, devtoolbar.New()).Handler()
	body := do(h, http.MethodGet, "/odd").Body.String()
	if !strings.Contains(body, "x&lt;y (en)") || strings.Contains(body, "x<y") {
		t.Errorf("page name not escaped:\n%s", body)
	}
}

// Every request is answered in full, so the panel is this response's.
func TestNoRevalidationUnderThePanel(t *testing.T) {
	h := app(t, true, devtoolbar.New()).Handler()
	etag := do(h, http.MethodGet, "/").Header().Get("ETag")
	if w := do(h, http.MethodGet, "/", "If-None-Match", etag); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "collage-devtoolbar") {
		t.Errorf("status %d", w.Code)
	}
}

func TestOtherResponsesPassThrough(t *testing.T) {
	h := app(t, true, devtoolbar.New()).Handler()
	if w := do(h, http.MethodGet, "/data.json"); w.Body.String() != `{"a":1}` || w.Header().Get("ETag") == "" {
		t.Errorf("document = %q", w.Body.String())
	}
	if w := do(h, http.MethodGet, "/raw/stream"); w.Body.String() != "data: 1\n\n" || w.Header().Get("X-Flush") != "" || !w.Flushed {
		t.Errorf("stream = %q, flushed %v", w.Body.String(), w.Flushed)
	}
	// A HEAD response has no body to put the panel in; what collage writes is
	// passed on for net/http to discard.
	if w := do(h, http.MethodHead, "/"); strings.Contains(w.Body.String(), "collage-devtoolbar") {
		t.Errorf("HEAD was given the panel: %q", w.Body.String())
	}
}

// A handler of the application's own that writes a page without saying it is
// HTML — net/http sniffs the type below the middleware — is given the panel too.
func TestUndeclaredHTML(t *testing.T) {
	h := app(t, true, devtoolbar.New()).Handler()
	w := do(h, http.MethodGet, "/undeclared")
	if !strings.Contains(w.Body.String(), `id="collage-devtoolbar"`) || !strings.HasSuffix(w.Body.String(), "</div></body></html>") {
		t.Errorf("no panel before </body>:\n%s", w.Body.String())
	}
	if w.Code != http.StatusAccepted || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") {
		t.Errorf("status %d, Content-Type %q", w.Code, w.Header().Get("Content-Type"))
	}
}

// Outside development the plugin is not there at all.
func TestNothingInProduction(t *testing.T) {
	with := do(app(t, false, devtoolbar.New()).Handler(), http.MethodGet, "/")
	without := do(app(t, false).Handler(), http.MethodGet, "/")
	if with.Body.String() != without.Body.String() || with.Header().Get("ETag") != without.Header().Get("ETag") {
		t.Errorf("production differs:\n%s\n%s", with.Body.String(), without.Body.String())
	}
	if strings.Contains(with.Body.String(), "collage-devtoolbar") {
		t.Error("panel in production")
	}
}

// A static build writes pages exactly as it would without the plugin, even from a
// development application.
func TestNothingInAStaticBuild(t *testing.T) {
	for _, dev := range []bool{false, true} {
		out := t.TempDir()
		b, err := collage.NewBuilder(app(t, dev, devtoolbar.New()), collage.BuildOptions{OutDir: out})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := b.Build(context.Background()); err != nil {
			t.Fatal(err)
		}
		written, err := os.ReadFile(filepath.Join(out, "index.html"))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(written), "collage-devtoolbar") {
			t.Errorf("dev=%v: panel in the build:\n%s", dev, written)
		}
	}
}
