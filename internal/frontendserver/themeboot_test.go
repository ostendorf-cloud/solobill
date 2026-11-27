package frontendserver_test

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/solobill/internal/frontendserver"
)

// bootScriptPattern matches the inline script govue injects into index.html.
var bootScriptPattern = regexp.MustCompile(`(?s)<script>(.*?)</script>`)

// TestIndexCarriesTheThemeBootScriptUnderItsHash pins that every route of the
// Vue router answers index.html with govue's theme boot script right after
// the head tag, and that script-src allows exactly that script by its hash
// and never unsafe-inline. The script applies the stored colour mode before
// the first paint.
func TestIndexCarriesTheThemeBootScriptUnderItsHash(t *testing.T) {
	t.Parallel()

	h := newTestHandler(t, testConfig(), frontendserver.Options{})

	for _, path := range []string{"/", "/de/customers", "/some/deep/route"} {
		rec := get(t, h, http.MethodGet, path)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d, want 200", path, rec.Code)
		}

		body := rec.Body.String()
		m := bootScriptPattern.FindStringSubmatch(body)
		if m == nil {
			t.Fatalf("GET %s: no boot script in the page: %s", path, body)
		}
		if !strings.Contains(m[1], "document.documentElement") || !strings.Contains(m[1], "'govue-theme'") {
			t.Errorf("GET %s: the script is not govue's theme boot script: %s", path, m[1])
		}
		if head := strings.Index(body, "<head>"); head < 0 || strings.Index(body, "<script") != head+len("<head>") {
			t.Errorf("GET %s: the script does not directly follow the head tag: %s", path, body)
		}

		sum := sha256.Sum256([]byte(m[1]))
		wantHash := "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
		src := scriptSrc(rec.Header().Get("Content-Security-Policy"))
		if !strings.Contains(src, wantHash) {
			t.Errorf("GET %s: script-src = %q, want it to carry %s", path, src, wantHash)
		}
		if strings.Contains(src, "unsafe-inline") {
			t.Errorf("GET %s: script-src = %q, want no unsafe-inline", path, src)
		}
	}
}
