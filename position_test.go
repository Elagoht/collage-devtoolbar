package devtoolbar_test

import (
	"encoding/json"
	"strings"
	"testing"

	devtoolbar "github.com/Elagoht/collage-devtoolbar"
	"github.com/Elagoht/collage/pkg/collage"
	"github.com/Elagoht/collage/pkg/collagetest"
)

// placement is the rule that puts the panel on the screen, the last one in its
// <style>: after the all:initial that would reset an earlier one.
func placement(t *testing.T, body string) string {
	t.Helper()
	end := strings.Index(body, "</style>")
	if end < 0 {
		t.Fatalf("no panel style in:\n%s", body)
	}
	rule := body[strings.LastIndex(body[:end], "#collage-devtoolbar{"):end]
	if strings.Contains(rule, "all:initial") {
		t.Fatalf("the placement %q is the rule all:initial resets", rule)
	}
	return rule
}

func TestPositionDefaultsToBottomLeft(t *testing.T) {
	body := do(app(t, true, devtoolbar.New(devtoolbar.Options{})).Handler(), "GET", "/").Body.String()
	if got, want := placement(t, body), "#collage-devtoolbar{bottom:8px;left:8px}"; got != want {
		t.Errorf("placement = %q, want %q", got, want)
	}
}

func TestEveryPosition(t *testing.T) {
	want := map[devtoolbar.Position]string{
		devtoolbar.PositionTopLeft:     "top:8px;left:8px",
		devtoolbar.PositionTop:         "top:8px;left:50%;transform:translateX(-50%)",
		devtoolbar.PositionTopRight:    "top:8px;right:8px",
		devtoolbar.PositionBottomRight: "bottom:8px;right:8px",
		devtoolbar.PositionBottom:      "bottom:8px;left:50%;transform:translateX(-50%)",
		devtoolbar.PositionBottomLeft:  "bottom:8px;left:8px",
	}
	for position, offsets := range want {
		body := do(app(t, true, devtoolbar.New(devtoolbar.Options{Position: position})).Handler(), "GET", "/").Body.String()
		if got := placement(t, body); got != "#collage-devtoolbar{"+offsets+"}" {
			t.Errorf("%s: placement = %q, want offsets %q", position, got, offsets)
		}
	}
}

// The plugin configuration overrides the position given in Go.
func TestPositionFromConfig(t *testing.T) {
	raw, err := json.Marshal(map[string]string{"position": "top-right"})
	if err != nil {
		t.Fatal(err)
	}
	h := appWith(t, true, func(c *collage.Config) {
		c.PluginConfig = map[string]json.RawMessage{devtoolbar.Name: raw}
	}, devtoolbar.New(devtoolbar.Options{Position: devtoolbar.PositionBottom})).Handler()
	if got := placement(t, do(h, "GET", "/").Body.String()); got != "#collage-devtoolbar{top:8px;right:8px}" {
		t.Errorf("placement = %q, want top-right's", got)
	}
}

// An unknown position fails Init, in production too, rather than drawing the
// panel somewhere nobody asked for.
func TestUnknownPositionFailsInit(t *testing.T) {
	for _, dev := range []bool{true, false} {
		_, err := collagetest.TrySite(t,
			collagetest.WithConfig(func(c *collage.Config) { c.DevMode = dev }),
			collagetest.WithPlugins(devtoolbar.New(devtoolbar.Options{})),
			collagetest.WithPluginConfig(devtoolbar.Name, `{"position":"middle"}`))
		if err == nil || !strings.Contains(err.Error(), `devtoolbar: unknown position "middle"; want top-left, top, top-right, bottom-right, bottom or bottom-left`) {
			t.Errorf("dev=%v: err = %v", dev, err)
		}
	}
	// The middle of a side edge is not a place for it.
	for _, position := range []devtoolbar.Position{"center", "left", "right"} {
		if _, err := collagetest.TrySite(t, collagetest.WithPlugins(devtoolbar.New(devtoolbar.Options{Position: position}))); err == nil {
			t.Errorf("the position %q set in Go was accepted", position)
		}
	}
}
