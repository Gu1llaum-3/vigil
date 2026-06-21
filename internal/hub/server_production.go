//go:build !development

package hub

import (
	"io/fs"
	"net/http"
	"strings"

	"github.com/Gu1llaum-3/vigil/internal/hub/utils"
	"github.com/Gu1llaum-3/vigil/internal/site"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// startServer sets up the production server.
func (h *Hub) startServer(se *core.ServeEvent) error {
	indexFile, _ := fs.ReadFile(site.DistDirFS, "index.html")
	html := modifyIndexHTML(h, indexFile)
	// set up static asset serving
	staticPaths := [2]string{"/static/", "/assets/"}
	serveStatic := apis.Static(site.DistDirFS, false)
	// get CSP configuration (custom CSP overrides the built-in default)
	csp, cspExists := utils.GetEnv("CSP")
	// add route
	se.Router.GET("/{path...}", func(e *core.RequestEvent) error {
		hdr := e.Response.Header()
		// Always-on, zero-risk hardening headers (apply to assets and HTML alike).
		hdr.Set("X-Content-Type-Options", "nosniff")
		hdr.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		// serve static assets if path is in staticPaths
		for i := range staticPaths {
			if strings.Contains(e.Request.URL.Path, staticPaths[i]) {
				hdr.Set("Cache-Control", "public, max-age=2592000")
				return serveStatic(e)
			}
		}
		if cspExists {
			// Operator-provided CSP takes full control (they own frame-ancestors).
			hdr.Del("X-Frame-Options")
			hdr.Set("Content-Security-Policy", csp)
			return e.HTML(http.StatusOK, html)
		}
		// Default: a nonce-based CSP so the inline bootstrap scripts run while arbitrary
		// injected scripts are blocked. Fail open (serve without CSP) if the nonce cannot
		// be generated, so a transient RNG error never takes the UI down.
		nonce, err := securityNonce()
		if err != nil {
			return e.HTML(http.StatusOK, html)
		}
		hdr.Set("X-Frame-Options", "SAMEORIGIN")
		hdr.Set("Content-Security-Policy", defaultContentSecurityPolicy(nonce))
		return e.HTML(http.StatusOK, addScriptNonce(html, nonce))
	})
	return nil
}
