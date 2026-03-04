package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/tobischo/gokeepasslib/v3"
	"github.com/tobischo/gokeepasslib/v3/wrappers"
)

// KeePassTool provides retrieve_password and store_password tools for a KeePass (.kdbx) database.
type KeePassTool struct {
	dbPath   string
	password string
}

// NewKeePassTool creates a new KeePassTool. dbPath is the path to the .kdbx file,
// password is the master password used to unlock it.
func NewKeePassTool(dbPath, password string) *KeePassTool {
	return &KeePassTool{dbPath: dbPath, password: password}
}

// EnsureDB ensures the KeePass database file exists. If it does not, it creates a new empty one.
func (kt *KeePassTool) EnsureDB() error {
	if kt.dbPath == "" {
		return fmt.Errorf("keepass db_path is not configured")
	}

	if _, err := os.Stat(kt.dbPath); err == nil {
		return nil // Already exists
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to check if KeePass database exists: %w", err)
	}

	db, err := kt.createNewDB()
	if err != nil {
		return err
	}

	if err := kt.saveDB(db); err != nil {
		return fmt.Errorf("failed to save new KeePass database: %w", err)
	}

	slog.Info("keepass: initialized new database", "path", kt.dbPath)
	return nil
}

// --- retrieve_password ---

type RetrievePasswordTool struct{ kt *KeePassTool }

func NewRetrievePasswordTool(dbPath, password string) *RetrievePasswordTool {
	return &RetrievePasswordTool{kt: NewKeePassTool(dbPath, password)}
}

func (r *RetrievePasswordTool) GetInfo() *schema.ToolInfo {
	return &schema.ToolInfo{
		Name: "retrieve_password",
		Desc: "Retrieve a password (and optionally username, URL, notes) from the KeePass database by entry title. Use this whenever you need a stored credential.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"title": {
				Type:     schema.String,
				Desc:     "The title of the KeePass entry to look up (case-insensitive substring match).",
				Required: true,
			},
			"include_username": {
				Type:     schema.Boolean,
				Desc:     "If true, include the username in the result.",
				Required: false,
			},
			"include_url": {
				Type:     schema.Boolean,
				Desc:     "If true, include the URL in the result.",
				Required: false,
			},
			"include_notes": {
				Type:     schema.Boolean,
				Desc:     "If true, include the notes in the result.",
				Required: false,
			},
		}),
	}
}

func (r *RetrievePasswordTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return r.GetInfo(), nil
}

func (r *RetrievePasswordTool) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (resStr string, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			slog.Error("panic in retrieve_password tool", "recover", rec)
			resStr = fmt.Sprintf("internal error: tool panicked: %v", rec)
			err = nil
		}
	}()

	var args struct {
		Title           string `json:"title"`
		IncludeUsername bool   `json:"include_username"`
		IncludeURL      bool   `json:"include_url"`
		IncludeNotes    bool   `json:"include_notes"`
	}
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if args.Title == "" {
		return "", fmt.Errorf("title is required")
	}

	db, err := r.kt.openDB()
	if err != nil {
		return "", err
	}

	entry := findEntry(db, args.Title)
	if entry == nil {
		return "", fmt.Errorf("no entry found with title matching %q", args.Title)
	}

	result := map[string]string{
		"title":    entry.GetTitle(),
		"password": entry.GetPassword(),
	}
	if args.IncludeUsername {
		result["username"] = entry.GetContent("UserName")
	}
	if args.IncludeURL {
		result["url"] = entry.GetContent("URL")
	}
	if args.IncludeNotes {
		result["notes"] = entry.GetContent("Notes")
	}

	out, _ := json.Marshal(result)
	slog.Info("retrieve_password: entry found", "title", entry.GetTitle())
	return string(out), nil
}

// --- store_password ---

type StorePasswordTool struct{ kt *KeePassTool }

func NewStorePasswordTool(dbPath, password string) *StorePasswordTool {
	return &StorePasswordTool{kt: NewKeePassTool(dbPath, password)}
}

func (s *StorePasswordTool) GetInfo() *schema.ToolInfo {
	return &schema.ToolInfo{
		Name: "store_password",
		Desc: "Store or update a password entry in the KeePass database. Creates a new entry or updates an existing one if update_if_exists is true.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"title": {
				Type:     schema.String,
				Desc:     "The title for the KeePass entry.",
				Required: true,
			},
			"password": {
				Type:     schema.String,
				Desc:     "The password to store.",
				Required: true,
			},
			"username": {
				Type:     schema.String,
				Desc:     "Optional username to store with the entry.",
				Required: false,
			},
			"url": {
				Type:     schema.String,
				Desc:     "Optional URL to store with the entry.",
				Required: false,
			},
			"notes": {
				Type:     schema.String,
				Desc:     "Optional notes to store with the entry.",
				Required: false,
			},
			"update_if_exists": {
				Type:     schema.Boolean,
				Desc:     "If true and an entry with the same title exists, update it. If false (default), return an error when a duplicate is found.",
				Required: false,
			},
		}),
	}
}

func (s *StorePasswordTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return s.GetInfo(), nil
}

func (s *StorePasswordTool) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (resStr string, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			slog.Error("panic in store_password tool", "recover", rec)
			resStr = fmt.Sprintf("internal error: tool panicked: %v", rec)
			err = nil
		}
	}()

	var args struct {
		Title          string `json:"title"`
		Password       string `json:"password"`
		Username       string `json:"username"`
		URL            string `json:"url"`
		Notes          string `json:"notes"`
		UpdateIfExists bool   `json:"update_if_exists"`
	}
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if args.Title == "" {
		return "", fmt.Errorf("title is required")
	}
	if args.Password == "" {
		return "", fmt.Errorf("password is required")
	}

	db, err := s.kt.openDB()
	if err != nil {
		return "", err
	}

	existing := findEntry(db, args.Title)
	if existing != nil {
		if !args.UpdateIfExists {
			return "", fmt.Errorf("entry with title %q already exists; set update_if_exists=true to overwrite", args.Title)
		}
		// Update in place
		setEntryValue(existing, "Password", args.Password)
		if args.Username != "" {
			setEntryValue(existing, "UserName", args.Username)
		}
		if args.URL != "" {
			setEntryValue(existing, "URL", args.URL)
		}
		if args.Notes != "" {
			setEntryValue(existing, "Notes", args.Notes)
		}
		slog.Info("store_password: updated existing entry", "title", args.Title)
	} else {
		// Create new entry in the root group
		entry := newEntry(args.Title, args.Password, args.Username, args.URL, args.Notes)
		if len(db.Content.Root.Groups) == 0 {
			db.Content.Root.Groups = []gokeepasslib.Group{gokeepasslib.NewGroup()}
		}
		db.Content.Root.Groups[0].Entries = append(db.Content.Root.Groups[0].Entries, entry)
		slog.Info("store_password: created new entry", "title", args.Title)
	}

	if err := s.kt.saveDB(db); err != nil {
		return "", fmt.Errorf("failed to save database: %w", err)
	}

	return fmt.Sprintf(`{"status":"ok","title":%q}`, args.Title), nil
}

// --- helpers ---

func (kt *KeePassTool) openDB() (*gokeepasslib.Database, error) {
	if kt.dbPath == "" {
		return nil, fmt.Errorf("keepass db_path is not configured")
	}

	f, err := os.Open(kt.dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open KeePass database: %w", err)
	}
	defer f.Close()

	db := gokeepasslib.NewDatabase()
	db.Credentials = gokeepasslib.NewPasswordCredentials(kt.password)
	if err := gokeepasslib.NewDecoder(f).Decode(db); err != nil {
		return nil, fmt.Errorf("failed to decode KeePass database: %w", err)
	}
	if err := db.UnlockProtectedEntries(); err != nil {
		return nil, fmt.Errorf("failed to unlock protected entries: %w", err)
	}
	return db, nil
}

func (kt *KeePassTool) createNewDB() (*gokeepasslib.Database, error) {
	db := gokeepasslib.NewDatabase()
	db.Credentials = gokeepasslib.NewPasswordCredentials(kt.password)
	rootGroup := gokeepasslib.NewGroup()
	rootGroup.Name = "Root"
	db.Content.Root.Groups = []gokeepasslib.Group{rootGroup}
	slog.Info("keepass: created new database", "path", kt.dbPath)
	return db, nil
}

func (kt *KeePassTool) saveDB(db *gokeepasslib.Database) error {
	if err := db.LockProtectedEntries(); err != nil {
		return fmt.Errorf("failed to lock protected entries: %w", err)
	}

	// Ensure the parent directory exists
	dir := filepath.Dir(kt.dbPath)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory for KeePass database: %w", err)
		}
	}

	f, err := os.OpenFile(kt.dbPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("failed to open KeePass database for writing: %w", err)
	}
	defer f.Close()

	return gokeepasslib.NewEncoder(f).Encode(db)
}

// findEntry searches all groups recursively for an entry whose title contains the query (case-insensitive).
func findEntry(db *gokeepasslib.Database, query string) *gokeepasslib.Entry {
	q := strings.ToLower(query)
	for i := range db.Content.Root.Groups {
		if e := searchGroup(&db.Content.Root.Groups[i], q); e != nil {
			return e
		}
	}
	return nil
}

func searchGroup(g *gokeepasslib.Group, query string) *gokeepasslib.Entry {
	for i := range g.Entries {
		if strings.Contains(strings.ToLower(g.Entries[i].GetTitle()), query) {
			return &g.Entries[i]
		}
	}
	for i := range g.Groups {
		if e := searchGroup(&g.Groups[i], query); e != nil {
			return e
		}
	}
	return nil
}

// setEntryValue updates or adds a value field in an entry.
func setEntryValue(e *gokeepasslib.Entry, key, value string) {
	for i := range e.Values {
		if e.Values[i].Key == key {
			e.Values[i].Value.Content = value
			return
		}
	}
	protected := key == "Password"
	e.Values = append(e.Values, gokeepasslib.ValueData{
		Key: key,
		Value: gokeepasslib.V{
			Content:   value,
			Protected: wrappers.NewBoolWrapper(protected),
		},
	})
}

// newEntry creates a new KeePass entry with the given fields.
func newEntry(title, password, username, url, notes string) gokeepasslib.Entry {
	e := gokeepasslib.NewEntry()
	now := time.Now()
	e.Times.CreationTime = &wrappers.TimeWrapper{Time: now}
	e.Times.LastModificationTime = &wrappers.TimeWrapper{Time: now}
	setEntryValue(&e, "Title", title)
	setEntryValue(&e, "Password", password)
	if username != "" {
		setEntryValue(&e, "UserName", username)
	}
	if url != "" {
		setEntryValue(&e, "URL", url)
	}
	if notes != "" {
		setEntryValue(&e, "Notes", notes)
	}
	return e
}
