// Package devtoolbar is a collage plugin that shows a small panel at the bottom of
// every page in development.
//
//	app, err := collage.New(&collage.Config{
//		DevMode: true,
//		Plugins: []collage.Plugin{ /* ...every other plugin... */ devtoolbar.New()},
//	})
//
// The panel says which page rendered and in which locale, the response's status,
// how long the render took and each fragment of it, which fragments failed, the
// dependency tags the render depended on, its Cache-Control and ETag, its size,
// and how many findings the plugins before it reported. Register it last: a render's findings
// are only those of the plugins that ran before it.
//
// Outside development it does nothing at all: no middleware is installed, no hook
// does any work, and a static build renders every page exactly as it would
// without it.
package devtoolbar

import (
	"bytes"
	"context"
	"fmt"
	"html"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Elagoht/collage/pkg/collage"
)

// Name is the plugin's name.
const Name = "elagoht/devtoolbar"

// renderTimeHeader is the header collage sets on a fresh render in development.
const renderTimeHeader = "X-Collage-Render-Time"

// Plugin draws the panel.
type Plugin struct {
	dev bool
}

// New returns the plugin. It has nothing to configure: in development it is on,
// and everywhere else it is off.
func New() *Plugin { return &Plugin{} }

func (p *Plugin) Name() string                   { return Name }
func (p *Plugin) Version() string                { return "0.2.4" }
func (p *Plugin) Shutdown(context.Context) error { return nil }

var (
	_ collage.Plugin          = (*Plugin)(nil)
	_ collage.AfterRenderHook = (*Plugin)(nil)
)

// Init installs the middleware in development, and nothing otherwise.
func (p *Plugin) Init(_ context.Context, host collage.Host) error {
	p.dev = host.DevMode()
	if !p.dev {
		return nil
	}
	return host.Use(p.middleware)
}

// record is what the render told the plugin about one request, carried from
// OnAfterRender, which sees the render, to the middleware, which sees the response.
type record struct {
	rendered bool
	page     string
	locale   string
	strategy string
	degraded bool
	errors   int
	warnings int
	// fragments and tags are the event's own copies, so the record may keep them.
	fragments []collage.FragmentReport
	tags      []string
}

type recordKey struct{}

// OnAfterRender notes the page, its locale, its fragments, its dependency tags
// and its findings in the request's record. The hook is handed the request's context, which is how the note reaches
// the middleware that wraps the same request.
func (p *Plugin) OnAfterRender(ctx context.Context, ev *collage.AfterRenderEvent) error {
	if !p.dev || ev.Static {
		return nil
	}
	rec, _ := ctx.Value(recordKey{}).(*record)
	if rec == nil {
		return nil
	}
	rec.rendered = true
	rec.locale = ev.Locale
	rec.degraded = ev.Degraded
	if ev.Page != nil {
		rec.page = ev.Page.Name
		rec.strategy = ev.Page.Strategy.String()
	}
	rec.fragments = ev.Fragments
	rec.tags = ev.DependencyTags
	rec.errors, rec.warnings = 0, 0
	for _, f := range ev.Findings {
		if f.Level == collage.FindingError {
			rec.errors++
		} else {
			rec.warnings++
		}
	}
	return nil
}

func (p *Plugin) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Every request is answered in full, so the panel on the page is the
		// panel for this response: a 304 would have the browser show the page it
		// kept, with the timings of whichever request first sent it.
		r.Header.Del("If-None-Match")
		r.Header.Del("If-Modified-Since")
		rec := &record{}
		bw := &bufferWriter{ResponseWriter: w, head: r.Method == http.MethodHead}
		next.ServeHTTP(bw, r.WithContext(context.WithValue(r.Context(), recordKey{}, rec)))
		bw.finish(rec)
	})
}

// bufferWriter holds back an HTML body to put the panel in it. Anything else — an
// event stream, an image, JSON, a compressed body — passes straight through, and
// so does a hijacked connection.
//
// A response is HTML when its Content-Type says so, or, when it has none, when
// its first bytes look like HTML: net/http sniffs a missing type the same way,
// below every middleware. Until those first bytes arrive, a status written
// without a type is held back rather than decided on.
type bufferWriter struct {
	http.ResponseWriter
	head      bool
	status    int
	decided   bool
	buffering bool
	body      bytes.Buffer
}

// decide settles whether the response is held back, from its Content-Type or,
// when it has none, from first, the first bytes written. The sniffed type is
// set on the response, so what net/http would have said is said here.
func (w *bufferWriter) decide(first []byte) {
	if w.decided {
		return
	}
	w.decided = true
	h := w.Header()
	if _, declared := h["Content-Type"]; !declared && len(first) > 0 && h.Get("Content-Encoding") == "" {
		h.Set("Content-Type", http.DetectContentType(first))
	}
	w.buffering = strings.HasPrefix(h.Get("Content-Type"), "text/html") && h.Get("Content-Encoding") == ""
	if !w.buffering && w.status != 0 {
		w.ResponseWriter.WriteHeader(w.status)
	}
}

func (w *bufferWriter) WriteHeader(status int) {
	// Informational: early hints, a protocol switch. Not the response itself.
	if status < http.StatusOK {
		w.ResponseWriter.WriteHeader(status)
		return
	}
	if w.decided {
		if !w.buffering {
			w.ResponseWriter.WriteHeader(status)
		} else if w.status == 0 {
			w.status = status
		}
		return
	}
	if w.status == 0 {
		w.status = status
	}
	if _, declared := w.Header()["Content-Type"]; declared {
		w.decide(nil)
	}
}

func (w *bufferWriter) Write(b []byte) (int, error) {
	w.decide(b)
	if w.buffering {
		return w.body.Write(b)
	}
	return w.ResponseWriter.Write(b)
}

// Flush passes a flush through when nothing is held back.
func (w *bufferWriter) Flush() {
	w.decide(nil)
	if !w.buffering {
		_ = http.NewResponseController(w.ResponseWriter).Flush()
	}
}

// Unwrap lets http.ResponseController reach the connection: a stream's write
// deadline, a WebSocket's hijack.
func (w *bufferWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *bufferWriter) finish(rec *record) {
	// Only a status was written, and nothing to sniff: it goes out as it was.
	w.decide(nil)
	if !w.buffering {
		return
	}
	status := w.status
	if status == 0 {
		status = http.StatusOK
	}
	body := w.body.Bytes()
	if !w.head {
		if at := lastBodyClose(body); at >= 0 {
			panel := render(rec, status, w.Header(), len(body))
			out := make([]byte, 0, len(body)+len(panel))
			out = append(out, body[:at]...)
			out = append(out, panel...)
			body = append(out, body[at:]...)
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	}
	w.ResponseWriter.WriteHeader(status)
	_, _ = w.ResponseWriter.Write(body)
}

// lastBodyClose finds the page's closing body tag, in any case. The last one, since
// a script or a comment earlier in the page may spell one out.
//
// Only ASCII is lowercased. bytes.ToLower does not keep lengths — Turkish İ is two
// bytes and lowercases to i, one — and an index into its result is not an index
// into body: each İ would put the panel a byte earlier, into the tag before.
func lastBodyClose(body []byte) int {
	lower := make([]byte, len(body))
	for i, c := range body {
		if 'A' <= c && c <= 'Z' {
			c += 'a' - 'A'
		}
		lower[i] = c
	}
	return bytes.LastIndex(lower, []byte("</body>"))
}

func render(rec *record, status int, h http.Header, size int) []byte {
	dash := func(s string) string {
		if s == "" {
			return "—"
		}
		return s
	}
	page := "not a page render"
	if rec.rendered {
		page = rec.page
		if rec.locale != "" {
			page += " (" + rec.locale + ")"
		}
	}
	findings := "—"
	if rec.rendered {
		findings = fmt.Sprintf("%d errors, %d warnings", rec.errors, rec.warnings)
	}
	renderTime := dash(h.Get(renderTimeHeader))

	failed := 0
	for _, f := range rec.fragments {
		if f.Failed {
			failed++
		}
	}

	summary := []string{page, strconv.Itoa(status), renderTime}
	if failed > 0 {
		summary = append(summary, plural(failed, "failed fragment"))
	}
	if rec.rendered && rec.errors+rec.warnings > 0 {
		summary = append(summary, strconv.Itoa(rec.errors+rec.warnings)+" findings")
	}

	rows := []row{
		{name: "Page", value: page},
		{name: "Strategy", value: dash(rec.strategy)},
		{name: "Status", value: strconv.Itoa(status) + " " + http.StatusText(status)},
		{name: "Render time", value: renderTime},
	}
	if rec.rendered {
		count := plural(len(rec.fragments), "fragment")
		if failed > 0 {
			count += ", " + strconv.Itoa(failed) + " failed"
		}
		rows = append(rows, row{name: "Fragments", value: count})
		// In the order they were entered: a fragment precedes the ones in its
		// slots, whose time its own includes.
		for _, f := range rec.fragments {
			rows = append(rows, row{name: f.Name, value: fragmentOutcome(f), sub: true, failed: f.Failed})
		}
		tags := "none"
		if len(rec.tags) > 0 {
			tags = strings.Join(rec.tags, ", ")
		}
		rows = append(rows, row{name: "Dependency tags", value: tags})
	}
	rows = append(rows,
		row{name: "Cache-Control", value: dash(h.Get("Cache-Control"))},
		row{name: "ETag", value: dash(h.Get("ETag"))},
		row{name: "Size", value: formatSize(size)},
		row{name: "Findings", value: findings},
	)
	if rec.degraded {
		rows = append(rows, row{name: "Degraded", value: "a fragment failed"})
	}

	var b strings.Builder
	b.WriteString(`<div id="collage-devtoolbar" role="region" aria-label="collage development toolbar">`)
	b.WriteString(style)
	b.WriteString(`<details><summary><span class="cdt-brand">collage</span> `)
	b.WriteString(html.EscapeString(strings.Join(summary, " · ")))
	b.WriteString(`</summary><dl>`)
	for _, r := range rows {
		class := ""
		switch {
		case r.sub && r.failed:
			class = ` class="cdt-sub cdt-failed"`
		case r.sub:
			class = ` class="cdt-sub"`
		}
		b.WriteString(`<dt` + class + `>` + html.EscapeString(r.name) + `</dt><dd` + class + `>` + html.EscapeString(r.value) + `</dd>`)
	}
	b.WriteString(`</dl></details>`)
	b.WriteString(`<button type="button" class="cdt-close" aria-label="Close the development toolbar" onclick="this.parentNode.remove()">×</button>`)
	b.WriteString(`</div>`)
	return []byte(b.String())
}

// row is one line of the panel. A sub row belongs to the one above it: a
// fragment under Fragments.
type row struct {
	name, value string
	sub, failed bool
}

// fragmentOutcome is a fragment's time, and how it failed if it did.
func fragmentOutcome(f collage.FragmentReport) string {
	out := f.Duration.Round(time.Microsecond).String()
	if !f.Failed {
		return out
	}
	out += " · failed"
	if f.UsedFallback {
		out += ", fallback shown"
	}
	if f.Err != nil {
		out += ": " + f.Err.Error()
	}
	return out
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(n) + " " + noun + "s"
}

func formatSize(n int) string {
	if n < 1024 {
		return strconv.Itoa(n) + " B"
	}
	return strconv.FormatFloat(float64(n)/1024, 'f', 1, 64) + " KB"
}

// style is scoped to the panel's id, and resets what a site's own stylesheet could
// have given its elements, so the panel looks the same on every page.
const style = `<style>
#collage-devtoolbar{all:initial;position:fixed;left:8px;bottom:8px;z-index:2147483647;display:flex;align-items:flex-start;gap:4px;max-width:calc(100vw - 16px);font:12px/1.5 ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;color:#e8e8e8;background:#1d1f21;border:1px solid #444;border-radius:6px;box-shadow:0 2px 8px rgba(0,0,0,.35)}
#collage-devtoolbar *{box-sizing:border-box;font:inherit;color:inherit;margin:0;padding:0}
#collage-devtoolbar details{padding:4px 8px;min-width:0}
#collage-devtoolbar summary{cursor:pointer;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
#collage-devtoolbar .cdt-brand{color:#8fd18f;font-weight:bold}
#collage-devtoolbar dl{display:grid;grid-template-columns:auto 1fr;gap:2px 12px;margin-top:6px;max-height:60vh;overflow:auto}
#collage-devtoolbar dt{color:#a0a0a0}
#collage-devtoolbar dd{word-break:break-all}
#collage-devtoolbar dt.cdt-sub{padding-left:12px}
#collage-devtoolbar .cdt-failed{color:#ff8a80}
#collage-devtoolbar button{cursor:pointer;background:none;border:0;padding:4px 8px;font-size:14px;line-height:1}
#collage-devtoolbar summary:focus-visible,#collage-devtoolbar button:focus-visible{outline:2px solid #8fd18f;outline-offset:1px}
</style>`
