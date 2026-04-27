package hub

import (
	"encoding/base64"
	"errors"
	"net/http"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type registryCredentialPayload struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Registry string `json:"registry"`
	Username string `json:"username"`
	Password string `json:"password"`
	Created  string `json:"created"`
	Updated  string `json:"updated"`
}

func registryCredentialResponse(rec *core.Record) registryCredentialPayload {
	return registryCredentialPayload{
		ID:       rec.Id,
		Name:     rec.GetString("name"),
		Registry: rec.GetString("registry"),
		Username: rec.GetString("username"),
		Password: redactedSecretMarker,
		Created:  rec.GetString("created"),
		Updated:  rec.GetString("updated"),
	}
}

func validateCredentialInput(name, registry, username string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("name is required")
	}
	if strings.TrimSpace(registry) == "" {
		return errors.New("registry is required")
	}
	if strings.TrimSpace(username) == "" {
		return errors.New("username is required")
	}
	return nil
}

func (h *Hub) listRegistryCredentials(e *core.RequestEvent) error {
	records, err := h.FindAllRecords(registryCredentialsCollection)
	if err != nil {
		return err
	}
	out := make([]registryCredentialPayload, 0, len(records))
	for _, rec := range records {
		out = append(out, registryCredentialResponse(rec))
	}
	return e.JSON(http.StatusOK, out)
}

func (h *Hub) createRegistryCredential(e *core.RequestEvent) error {
	var body registryCredentialPayload
	if err := e.BindBody(&body); err != nil {
		return e.BadRequestError("Invalid request body", err)
	}
	body.Name = strings.TrimSpace(body.Name)
	body.Registry = normalizeRegistry(strings.TrimSpace(body.Registry))
	body.Username = strings.TrimSpace(body.Username)
	if err := validateCredentialInput(body.Name, body.Registry, body.Username); err != nil {
		return e.BadRequestError(err.Error(), err)
	}
	if body.Password == "" || body.Password == redactedSecretMarker {
		return e.BadRequestError("Password is required", nil)
	}

	collection, err := h.FindCachedCollectionByNameOrId(registryCredentialsCollection)
	if err != nil {
		return err
	}
	rec := core.NewRecord(collection)
	rec.Set("name", body.Name)
	rec.Set("registry", body.Registry)
	rec.Set("username", body.Username)

	ciphertext, nonce, err := encryptSecret(h.credentialsKey, []byte(body.Password))
	if err != nil {
		return err
	}
	rec.Set("password_ciphertext", base64.StdEncoding.EncodeToString(ciphertext))
	rec.Set("password_nonce", base64.StdEncoding.EncodeToString(nonce))

	if err := h.Save(rec); err != nil {
		return e.BadRequestError("Failed to save credential (registry may already be in use)", err)
	}
	return e.JSON(http.StatusOK, registryCredentialResponse(rec))
}

func (h *Hub) updateRegistryCredential(e *core.RequestEvent) error {
	id := e.Request.PathValue("id")
	rec, err := h.FindFirstRecordByFilter(registryCredentialsCollection, "id = {:id}", dbx.Params{"id": id})
	if err != nil {
		return e.NotFoundError("Credential not found", err)
	}

	var body registryCredentialPayload
	if err := e.BindBody(&body); err != nil {
		return e.BadRequestError("Invalid request body", err)
	}
	body.Name = strings.TrimSpace(body.Name)
	body.Registry = normalizeRegistry(strings.TrimSpace(body.Registry))
	body.Username = strings.TrimSpace(body.Username)
	if err := validateCredentialInput(body.Name, body.Registry, body.Username); err != nil {
		return e.BadRequestError(err.Error(), err)
	}

	rec.Set("name", body.Name)
	rec.Set("registry", body.Registry)
	rec.Set("username", body.Username)

	if body.Password != "" && body.Password != redactedSecretMarker {
		ciphertext, nonce, err := encryptSecret(h.credentialsKey, []byte(body.Password))
		if err != nil {
			return err
		}
		rec.Set("password_ciphertext", base64.StdEncoding.EncodeToString(ciphertext))
		rec.Set("password_nonce", base64.StdEncoding.EncodeToString(nonce))
	}

	if err := h.Save(rec); err != nil {
		return e.BadRequestError("Failed to update credential (registry may already be in use)", err)
	}
	return e.JSON(http.StatusOK, registryCredentialResponse(rec))
}

func (h *Hub) deleteRegistryCredential(e *core.RequestEvent) error {
	id := e.Request.PathValue("id")
	rec, err := h.FindFirstRecordByFilter(registryCredentialsCollection, "id = {:id}", dbx.Params{"id": id})
	if err != nil {
		return e.NotFoundError("Credential not found", err)
	}
	if err := h.Delete(rec); err != nil {
		return err
	}
	return e.JSON(http.StatusOK, map[string]any{"ok": true})
}
