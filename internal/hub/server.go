package hub

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"

	"github.com/Gu1llaum-3/vigil"
	"github.com/Gu1llaum-3/vigil/internal/hub/utils"
)

// PublicAppInfo defines the structure of the public app information that will be injected into the HTML
type PublicAppInfo struct {
	BASE_PATH           string
	DISPLAY_NAME        string
	HUB_VERSION         string
	HUB_URL             string
	OAUTH_DISABLE_POPUP bool `json:"OAUTH_DISABLE_POPUP,omitempty"`
}

// modifyIndexHTML injects the public app information into the index.html content
func modifyIndexHTML(hub *Hub, html []byte) string {
	info := getPublicAppInfo(hub)
	content, err := json.Marshal(info)
	if err != nil {
		return string(html)
	}
	htmlContent := strings.ReplaceAll(string(html), "./", info.BASE_PATH)
	return strings.Replace(htmlContent, "\"{info}\"", string(content), 1)
}

func getPublicAppInfo(hub *Hub) PublicAppInfo {
	parsedURL, _ := url.Parse(hub.appURL)
	info := PublicAppInfo{
		BASE_PATH:    strings.TrimSuffix(parsedURL.Path, "/") + "/",
		DISPLAY_NAME: app.DisplayName,
		HUB_VERSION:  app.Version,
		HUB_URL:      hub.appURL,
	}
	if val, _ := utils.GetEnv("OAUTH_DISABLE_POPUP"); val == "true" {
		info.OAUTH_DISABLE_POPUP = true
	}
	return info
}

// securityNonce returns a fresh base64 CSP nonce (128 bits of entropy).
func securityNonce() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

// addScriptNonce tags the index.html's inline <script> blocks with the CSP nonce so they
// are allowed under a script-src 'nonce-…' policy. Only bare "<script>" tags are inline
// scripts; the bundled module script ("<script type=…") is served from 'self' and is left
// untouched.
func addScriptNonce(html, nonce string) string {
	return strings.ReplaceAll(html, "<script>", "<script nonce=\""+nonce+"\">")
}

// defaultContentSecurityPolicy returns the baseline CSP applied when the operator has not
// set a custom CSP env var. Scripts are locked to same-origin plus the per-request nonce
// (the inline bootstrap scripts carry it); styles allow inline (the index ships an inline
// <style> and UI libs set style attributes); images allow https + data so OAuth-provider
// avatars load; everything else is same-origin, framing is same-origin only, and plugins
// are disabled.
func defaultContentSecurityPolicy(nonce string) string {
	return strings.Join([]string{
		"default-src 'self'",
		"script-src 'self' 'nonce-" + nonce + "'",
		"style-src 'self' 'unsafe-inline'",
		"img-src 'self' data: https:",
		"font-src 'self' data:",
		"connect-src 'self'",
		"frame-ancestors 'self'",
		"base-uri 'self'",
		"form-action 'self'",
		"object-src 'none'",
	}, "; ")
}
