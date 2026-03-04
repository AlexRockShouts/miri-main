package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestStoreAndRetrievePassword(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.kdbx")
	password := "master-password"

	storeTool := NewStorePasswordTool(dbPath, password)
	kt := NewKeePassTool(dbPath, password)
	if err := kt.EnsureDB(); err != nil {
		t.Fatalf("failed to ensure DB: %v", err)
	}
	retrieveTool := NewRetrievePasswordTool(dbPath, password)
	ctx := t.Context()

	// Store a new entry
	res, err := storeTool.InvokableRun(ctx, `{"title":"GitHub","password":"gh-secret","username":"alice","url":"https://github.com","notes":"work account"}`)
	if err != nil {
		t.Fatalf("store_password failed: %v", err)
	}
	if res == "" {
		t.Fatal("store_password returned empty result")
	}

	// Retrieve it back
	res, err = retrieveTool.InvokableRun(ctx, `{"title":"GitHub","include_username":true,"include_url":true,"include_notes":true}`)
	if err != nil {
		t.Fatalf("retrieve_password failed: %v", err)
	}
	if res == "" {
		t.Fatal("retrieve_password returned empty result")
	}
	t.Logf("retrieve result: %s", res)

	// Verify the file was created
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("kdbx file not created: %v", err)
	}
}

func TestEnsureDB(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "new-test.kdbx")
	password := "master-password"

	kt := NewKeePassTool(dbPath, password)

	// Verify file does not exist
	if _, err := os.Stat(dbPath); err == nil {
		t.Fatal("kdbx file already exists")
	}

	// Ensure DB
	if err := kt.EnsureDB(); err != nil {
		t.Fatalf("EnsureDB failed: %v", err)
	}

	// Verify file was created
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("kdbx file not created: %v", err)
	}

	// Ensure it can be opened
	db, err := kt.openDB()
	if err != nil {
		t.Fatalf("failed to open newly created DB: %v", err)
	}
	if len(db.Content.Root.Groups) == 0 {
		t.Fatal("newly created DB has no root groups")
	}
	if db.Content.Root.Groups[0].Name != "Root" {
		t.Errorf("expected group name 'Root', got %q", db.Content.Root.Groups[0].Name)
	}

	// Call EnsureDB again, it should not fail
	if err := kt.EnsureDB(); err != nil {
		t.Fatalf("EnsureDB failed on second call: %v", err)
	}
}

func TestSaveDB_CreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "nested", "dir", "test.kdbx")
	kt := NewKeePassTool(dbPath, "pass")

	db, _ := kt.createNewDB()
	if err := kt.saveDB(db); err != nil {
		t.Fatalf("saveDB failed to create nested directories: %v", err)
	}

	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("kdbx file not created in nested directory: %v", err)
	}
}

func TestStorePassword_DuplicateWithoutUpdate(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.kdbx")
	kt := NewKeePassTool(dbPath, "pass")
	if err := kt.EnsureDB(); err != nil {
		t.Fatalf("failed to ensure DB: %v", err)
	}
	storeTool := NewStorePasswordTool(dbPath, "pass")
	ctx := t.Context()

	_, err := storeTool.InvokableRun(ctx, `{"title":"MyEntry","password":"secret1"}`)
	if err != nil {
		t.Fatalf("first store failed: %v", err)
	}

	_, err = storeTool.InvokableRun(ctx, `{"title":"MyEntry","password":"secret2"}`)
	if err == nil {
		t.Fatal("expected error on duplicate entry without update_if_exists, got nil")
	}
}

func TestStorePassword_UpdateIfExists(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.kdbx")
	kt := NewKeePassTool(dbPath, "pass")
	if err := kt.EnsureDB(); err != nil {
		t.Fatalf("failed to ensure DB: %v", err)
	}
	storeTool := NewStorePasswordTool(dbPath, "pass")
	retrieveTool := NewRetrievePasswordTool(dbPath, "pass")
	ctx := t.Context()

	_, err := storeTool.InvokableRun(ctx, `{"title":"MyEntry","password":"old-secret"}`)
	if err != nil {
		t.Fatalf("first store failed: %v", err)
	}

	_, err = storeTool.InvokableRun(ctx, `{"title":"MyEntry","password":"new-secret","update_if_exists":true}`)
	if err != nil {
		t.Fatalf("update store failed: %v", err)
	}

	res, err := retrieveTool.InvokableRun(ctx, `{"title":"MyEntry"}`)
	if err != nil {
		t.Fatalf("retrieve failed: %v", err)
	}
	t.Logf("updated entry: %s", res)
}

func TestRetrievePassword_NotFound(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.kdbx")
	// Store one entry first so the DB exists
	storeTool := NewStorePasswordTool(dbPath, "pass")
	ctx := t.Context()
	_, _ = storeTool.InvokableRun(ctx, `{"title":"SomeEntry","password":"x"}`)

	retrieveTool := NewRetrievePasswordTool(dbPath, "pass")
	_, err := retrieveTool.InvokableRun(context.Background(), `{"title":"NonExistent"}`)
	if err == nil {
		t.Fatal("expected error for missing entry, got nil")
	}
}

func TestStorePassword_MissingTitle(t *testing.T) {
	dir := t.TempDir()
	storeTool := NewStorePasswordTool(filepath.Join(dir, "test.kdbx"), "pass")
	_, err := storeTool.InvokableRun(t.Context(), `{"password":"secret"}`)
	if err == nil {
		t.Fatal("expected error for missing title")
	}
}

func TestStorePassword_MissingPassword(t *testing.T) {
	dir := t.TempDir()
	storeTool := NewStorePasswordTool(filepath.Join(dir, "test.kdbx"), "pass")
	_, err := storeTool.InvokableRun(t.Context(), `{"title":"MyEntry"}`)
	if err == nil {
		t.Fatal("expected error for missing password")
	}
}
