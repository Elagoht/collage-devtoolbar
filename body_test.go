package devtoolbar

import "testing"

// Turkish İ is two bytes and lowercases to one: text before the tag must not move
// where the panel lands.
func TestLastBodyCloseAfterNonASCIIText(t *testing.T) {
	body := []byte("<main>İzmir İstanbul</main></BODY>")
	if got, want := lastBodyClose(body), len(body)-len("</BODY>"); got != want {
		t.Errorf("lastBodyClose = %d, want %d", got, want)
	}
}
