package devtoolbar_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	devtoolbar "github.com/Elagoht/collage-devtoolbar"
	"github.com/Elagoht/collage/pkg/collage"
)

// from requests path as a client at addr would.
func from(h http.Handler, addr, path string, header ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r.RemoteAddr = addr
	for i := 0; i+1 < len(header); i += 2 {
		r.Header.Set(header[i], header[i+1])
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

const panelMark = `<div id="collage-devtoolbar"`

// The panel is for the developer's machine and the networks it sits on: loopback,
// a Docker bridge, the LAN a phone tests from.
func TestPanelForLocalPeers(t *testing.T) {
	h := app(t, true, devtoolbar.New()).Handler()
	for _, addr := range []string{
		"127.0.0.1:1", "127.8.9.10:1", "[::1]:1",
		"10.0.0.5:1", "172.17.0.1:1", "192.168.1.20:1",
		"[fd00::1]:1", "[fe80::1]:1", "169.254.10.1:1",
		"[::ffff:127.0.0.1]:1",
	} {
		if w := from(h, addr, "/"); !strings.Contains(w.Body.String(), panelMark) {
			t.Errorf("%s: no panel", addr)
		}
	}
}

// A peer on the public internet means development mode is serving the world: the
// response goes out exactly as it would without the plugin, and the request
// reaches the application with its conditional headers.
func TestRemotePeerUntouched(t *testing.T) {
	plain := app(t, true).Handler()
	h := app(t, true, devtoolbar.New()).Handler()
	for _, addr := range []string{"203.0.113.5:4000", "[2001:db8::5]:4000", "172.32.0.1:1", "garbage"} {
		if w := from(h, addr, "/"); w.Code != http.StatusOK || strings.Contains(w.Body.String(), panelMark) {
			t.Errorf("%s: %d, panel %v", addr, w.Code, strings.Contains(w.Body.String(), panelMark))
		}
		want, got := from(plain, addr, "/undeclared"), from(h, addr, "/undeclared")
		if got.Code != want.Code || got.Body.String() != want.Body.String() ||
			got.Header().Get("Content-Type") != want.Header().Get("Content-Type") ||
			got.Header().Get("Content-Length") != want.Header().Get("Content-Length") {
			t.Errorf("%s: handler's response changed:\n%d %v %q\nwant\n%d %v %q", addr,
				got.Code, got.Header(), got.Body.String(), want.Code, want.Header(), want.Body.String())
		}
		if w := from(h, addr, "/inm", "If-None-Match", `"v1"`); !strings.Contains(w.Body.String(), "&#34;v1&#34;") {
			t.Errorf("%s: If-None-Match did not reach the handler: %q", addr, w.Body.String())
		}
	}
	// A local peer still has it taken away, so the panel is this response's.
	if w := from(h, "127.0.0.1:1", "/inm", "If-None-Match", `"v1"`); strings.Contains(w.Body.String(), "v1") {
		t.Error("If-None-Match reached the handler for a local peer")
	}
}

// The first remote peer is warned about, once.
func TestRemotePeerWarnsOnce(t *testing.T) {
	var buf bytes.Buffer
	h := appWith(t, true, func(c *collage.Config) {
		c.Logger = slog.New(slog.NewTextHandler(&buf, nil))
	}, devtoolbar.New()).Handler()
	from(h, "127.0.0.1:1", "/")
	if strings.Contains(buf.String(), "devtoolbar") {
		t.Fatalf("warned for a local peer:\n%s", buf.String())
	}
	for range 3 {
		from(h, "203.0.113.5:1", "/")
	}
	if n := strings.Count(buf.String(), "devtoolbar: DevMode is serving a non-local peer"); n != 1 {
		t.Errorf("%d warnings, want 1:\n%s", n, buf.String())
	}
}

// AllowRemote turns the check off, from Go or from configuration.
func TestAllowRemote(t *testing.T) {
	fromGo := app(t, true, devtoolbar.NewWith(devtoolbar.Options{AllowRemote: true})).Handler()
	raw, err := json.Marshal(map[string]bool{"allowRemote": true})
	if err != nil {
		t.Fatal(err)
	}
	fromConfig := appWith(t, true, func(c *collage.Config) {
		c.PluginConfig = map[string]json.RawMessage{devtoolbar.Name: raw}
	}, devtoolbar.New()).Handler()
	for name, h := range map[string]http.Handler{"go": fromGo, "config": fromConfig} {
		if w := from(h, "203.0.113.5:1", "/"); !strings.Contains(w.Body.String(), panelMark) {
			t.Errorf("%s: no panel for a remote peer with AllowRemote", name)
		}
	}
}

// Behind a proxy collage trusts, the client is the one the proxy names: a public
// visitor reaching a development server through nginx on the same machine gets
// no panel. From a peer that is not trusted, X-Forwarded-For is a header anyone
// can write, so it changes nothing.
func TestTrustedProxyNamesTheClient(t *testing.T) {
	h := appWith(t, true, func(c *collage.Config) {
		c.Server.TrustedProxies = []string{"127.0.0.1"}
	}, devtoolbar.New()).Handler()
	if w := from(h, "127.0.0.1:1", "/", "X-Forwarded-For", "203.0.113.5"); strings.Contains(w.Body.String(), panelMark) {
		t.Error("panel for a public client behind a trusted proxy")
	}
	if w := from(h, "127.0.0.1:1", "/", "X-Forwarded-For", "192.168.1.20"); !strings.Contains(w.Body.String(), panelMark) {
		t.Error("no panel for a LAN client behind a trusted proxy")
	}
	if w := from(h, "10.0.0.7:1", "/", "X-Forwarded-For", "203.0.113.5"); !strings.Contains(w.Body.String(), panelMark) {
		t.Error("a forged X-Forwarded-For from an untrusted peer hid the panel")
	}
}
