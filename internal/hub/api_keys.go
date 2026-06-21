package hub

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
	"github.com/pocketbase/pocketbase/tools/types"
)

const (
	userApiKeysCollection = "user_api_keys"
	apiKeyTokenPrefix     = "vk_"
	apiKeyRandomLen       = 40
)

// hashApiKey returns the hex SHA-256 of a token. API keys are high-entropy random strings,
// so a fast hash (not bcrypt) is the right choice — it also lets us look a key up by hash.
func hashApiKey(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// authenticateApiKey is a global middleware that authenticates a request bearing a Vigil
// API key ("Authorization: Bearer vk_..."). It resolves the owning user, enforces the key's
// scope (a read key may only perform safe/read methods, on ANY route), records the key
// context for downstream guards, and replaces the Authorization header with a freshly
// minted user JWT so PocketBase's RequireAuth validates cleanly. A non-vk_ token, an
// unknown/expired key, or an already-authenticated request all fall through untouched.
func (h *Hub) authenticateApiKey(e *core.RequestEvent) error {
	if e.Auth != nil {
		return e.Next()
	}
	token := strings.TrimSpace(strings.TrimPrefix(e.Request.Header.Get("Authorization"), "Bearer "))
	if !strings.HasPrefix(token, apiKeyTokenPrefix) {
		return e.Next()
	}
	rec, err := h.FindFirstRecordByFilter(userApiKeysCollection, "token_hash = {:h}", dbx.Params{"h": hashApiKey(token)})
	if err != nil {
		return e.Next() // unknown key → fall through; the normal auth will reject with 401
	}
	if exp := rec.GetDateTime("expires_at"); !exp.IsZero() && exp.Time().Before(time.Now()) {
		return e.Next() // expired
	}
	user, err := h.FindRecordById("users", rec.GetString("created_by"))
	if err != nil {
		return e.Next()
	}
	scope := rec.GetString("scope")
	if scope == "" {
		scope = "read"
	}
	// Enforce scope at the auth point: a read-only key may only perform safe methods. This
	// covers every route (incl. the generic /api/collections API), not just /api/app.
	if scope == "read" && e.Request.Method != http.MethodGet && e.Request.Method != http.MethodHead {
		return e.ForbiddenError("This API key is read-only.", nil)
	}
	e.Auth = user
	e.Set("authViaApiKey", true)
	e.Set("apiKeyScope", scope)
	// Hand the downstream RequireAuth a valid JWT so it doesn't try to parse the vk_ token.
	if jwt, jwtErr := user.NewAuthToken(); jwtErr == nil {
		e.Request.Header.Set("Authorization", jwt)
	}
	h.touchApiKeyLastUsed(rec)
	return e.Next()
}

// touchApiKeyLastUsed records last_used_at, throttled to at most once per minute so a busy
// key does not cause a DB write on every request. Best-effort: failures are ignored.
func (h *Hub) touchApiKeyLastUsed(rec *core.Record) {
	if last := rec.GetDateTime("last_used_at"); !last.IsZero() && time.Since(last.Time()) < time.Minute {
		return
	}
	rec.Set("last_used_at", types.NowDateTime())
	_ = h.SaveNoValidate(rec)
}

// authViaApiKey reports whether the current request authenticated via an API key (vs a real
// user session). Key management must require a real session, so a key cannot mint more keys.
func authViaApiKey(e *core.RequestEvent) bool {
	v, _ := e.Get("authViaApiKey").(bool)
	return v
}

type apiKeyPayload struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Prefix     string `json:"prefix"`
	Scope      string `json:"scope"`
	LastUsedAt string `json:"last_used_at"`
	ExpiresAt  string `json:"expires_at"`
	Created    string `json:"created"`
}

func apiKeyToPayload(rec *core.Record) apiKeyPayload {
	return apiKeyPayload{
		ID:         rec.Id,
		Name:       rec.GetString("name"),
		Prefix:     rec.GetString("prefix"),
		Scope:      rec.GetString("scope"),
		LastUsedAt: rec.GetString("last_used_at"),
		ExpiresAt:  rec.GetString("expires_at"),
		Created:    rec.GetString("created"),
	}
}

// listApiKeys returns the current user's API keys (metadata only — never the token or hash).
func (h *Hub) listApiKeys(e *core.RequestEvent) error {
	records, err := h.FindAllRecords(userApiKeysCollection, dbx.HashExp{"created_by": e.Auth.Id})
	if err != nil {
		return e.InternalServerError("Internal server error", err)
	}
	out := make([]apiKeyPayload, 0, len(records))
	for _, rec := range records {
		out = append(out, apiKeyToPayload(rec))
	}
	return e.JSON(http.StatusOK, out)
}

// createApiKey mints a new key for the current user and returns the plaintext token ONCE.
func (h *Hub) createApiKey(e *core.RequestEvent) error {
	if authViaApiKey(e) {
		return e.ForbiddenError("API keys cannot be managed with an API key.", nil)
	}
	var body struct {
		Name      string `json:"name"`
		Scope     string `json:"scope"`
		ExpiresAt string `json:"expires_at"`
	}
	if err := e.BindBody(&body); err != nil {
		return e.BadRequestError("Invalid request body", err)
	}
	if strings.TrimSpace(body.Name) == "" {
		return e.BadRequestError("A name is required", nil)
	}
	scope := body.Scope
	if scope != "read" && scope != "read-write" {
		scope = "read"
	}

	token := apiKeyTokenPrefix + security.RandomString(apiKeyRandomLen)
	collection, err := h.FindCachedCollectionByNameOrId(userApiKeysCollection)
	if err != nil {
		return e.InternalServerError("Internal server error", err)
	}
	rec := core.NewRecord(collection)
	rec.Set("name", strings.TrimSpace(body.Name))
	rec.Set("created_by", e.Auth.Id)
	rec.Set("token_hash", hashApiKey(token))
	rec.Set("prefix", token[:min(11, len(token))])
	rec.Set("scope", scope)
	if body.ExpiresAt != "" {
		rec.Set("expires_at", body.ExpiresAt)
	}
	if err := h.Save(rec); err != nil {
		return e.BadRequestError("Failed to create API key", err)
	}
	// The plaintext token is returned exactly once here and never again.
	return e.JSON(http.StatusOK, map[string]any{"key": apiKeyToPayload(rec), "token": token})
}

// deleteApiKey revokes one of the current user's keys.
func (h *Hub) deleteApiKey(e *core.RequestEvent) error {
	if authViaApiKey(e) {
		return e.ForbiddenError("API keys cannot be managed with an API key.", nil)
	}
	id := e.Request.PathValue("id")
	rec, err := h.FindFirstRecordByFilter(userApiKeysCollection, "id = {:id} && created_by = {:u}", dbx.Params{"id": id, "u": e.Auth.Id})
	if err != nil {
		return e.NotFoundError("API key not found", err)
	}
	if err := h.Delete(rec); err != nil {
		return e.InternalServerError("Internal server error", err)
	}
	return e.JSON(http.StatusOK, map[string]any{"ok": true})
}
