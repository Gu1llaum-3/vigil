//go:build testing

// Package tests provides helpers for testing the application.
package tests

import (
	"fmt"
	"testing"

	"github.com/Gu1llaum-3/vigil/internal/hub"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
	"github.com/stretchr/testify/assert"

	_ "github.com/pocketbase/pocketbase/migrations"
)

// TestHub is a wrapper hub instance used for testing.
type TestHub struct {
	core.App
	*tests.TestApp
	*hub.Hub
}

// NewTestHub creates and initializes a test application instance.
//
// It is the caller's responsibility to call app.Cleanup() when the app is no longer needed.
func NewTestHub(optTestDataDir ...string) (*TestHub, error) {
	var testDataDir string
	if len(optTestDataDir) > 0 {
		testDataDir = optTestDataDir[0]
	}

	return NewTestHubWithConfig(core.BaseAppConfig{
		DataDir:       testDataDir,
		EncryptionEnv: "pb_test_env",
	})
}

// NewTestHubWithConfig creates and initializes a test application instance
// from the provided config.
//
// If config.DataDir is not set it fallbacks to the default internal test data directory.
//
// config.DataDir is cloned for each new test application instance.
//
// It is the caller's responsibility to call app.Cleanup() when the app is no longer needed.
func NewTestHubWithConfig(config core.BaseAppConfig) (*TestHub, error) {
	testApp, err := tests.NewTestAppWithConfig(config)
	if err != nil {
		return nil, err
	}

	h := hub.NewHub(testApp)

	t := &TestHub{
		App:     testApp,
		TestApp: testApp,
		Hub:     h,
	}

	return t, nil
}

// Helper function to create a test user for config tests
func CreateUser(app core.App, email string, password string) (*core.Record, error) {
	userCollection, err := app.FindCachedCollectionByNameOrId("users")
	if err != nil {
		return nil, err
	}

	user := core.NewRecord(userCollection)
	user.Set("email", email)
	user.Set("password", password)

	return user, app.Save(user)
}

// Helper function to create a test superuser for config tests
func CreateSuperuser(app core.App, email string, password string) (*core.Record, error) {
	superusersCollection, _ := app.FindCachedCollectionByNameOrId(core.CollectionNameSuperusers)
	superuser := core.NewRecord(superusersCollection)
	superuser.Set("email", email)
	superuser.Set("password", password)

	return superuser, app.Save(superuser)
}

func CreateUserWithRole(app core.App, email string, password string, roleName string) (*core.Record, error) {
	user, err := CreateUser(app, email, password)
	if err != nil {
		return nil, err
	}

	user.Set("role", roleName)
	return user, app.Save(user)
}

// Helper function to create a test record
func CreateRecord(app core.App, collectionName string, fields map[string]any) (*core.Record, error) {
	collection, err := app.FindCachedCollectionByNameOrId(collectionName)
	if err != nil {
		return nil, err
	}

	record := core.NewRecord(collection)
	record.Load(fields)

	return record, app.Save(record)
}

func ClearCollection(t testing.TB, app core.App, collectionName string) error {
	_, err := app.DB().NewQuery(fmt.Sprintf("DELETE from %s", collectionName)).Execute()
	recordCount, err := app.CountRecords(collectionName)
	assert.EqualValues(t, recordCount, 0, "should have 0 records after clearing")
	return err
}

func (h *TestHub) Cleanup() {
	h.TestApp.Cleanup()
}

// GetHubWithUser creates a test hub with a test user
func GetHubWithUser(t *testing.T) (*TestHub, *core.Record) {
	hub, err := NewTestHub(t.TempDir())
	assert.NoError(t, err)
	hub.StartHub()

	// Create a test user
	user, err := CreateUser(hub, "test@example.com", "password")
	assert.NoError(t, err)

	return hub, user
}
