package gpt

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/davecgh/go-spew/spew"
	"github.com/golang-migrate/migrate/v4"
	"github.com/google/wire"
	"github.com/hayeah/goo"
	"github.com/jmoiron/sqlx"
	"github.com/sashabaranov/go-openai"

	_ "github.com/golang-migrate/migrate/v4/database/sqlite3"
	_ "github.com/mattn/go-sqlite3"
)

type Config struct {
	goo.Config
	OpenAI OpenAIConfig
}

type OpenAIConfig struct {
	APIKey string
}

type PushCmd struct {
	Remote      string `arg:"positional"`
	Branch      string `arg:"positional"`
	SetUpstream bool   `arg:"-u"`
}

type RunCmd struct {
	Message string `arg:"positional,required"`
}

type ThreadMessagesCmd struct {
}

type ThreadUseCmd struct {
	ID string `arg:"positional,required"`
}

type ThreadScopeCmd struct {
	Messages *ThreadMessagesCmd `arg:"subcommand:messages" help:"list messages"`
	Use      *ThreadUseCmd      `arg:"subcommand:use" help:"use thread"`
}

type AssistantListCmd struct {
	// Remote      string `arg:"positional"`
}

type AssistantUseCmd struct {
	ID string `arg:"positional,required"`
}

type AssistantCreateCmd struct {
	AssistantFile string `arg:"positional,required"`
}

type AssistantScopeCmd struct {
	List   *AssistantListCmd   `arg:"subcommand:ls" help:"list assistants"`
	Use    *AssistantUseCmd    `arg:"subcommand:use" help:"use assistant"`
	Create *AssistantCreateCmd `arg:"subcommand:create" help:"create assistant"`
}

type Args struct {
	Assistant *AssistantScopeCmd `arg:"subcommand:assistant" help:"manage assistants"`
	Run       *RunCmd            `arg:"subcommand:run" help:"run a thread"`
	Thread    *ThreadScopeCmd    `arg:"subcommand:thread" help:"manage threads"`
}

type App struct {
	Args         *Args
	Config       *Config
	OAI          *openai.Client
	AM           *AssistantManager
	TM           *ThreadManager
	ThreadRunner *ThreadRunner
	Migrate      *migrate.Migrate
}

// hashAssistantRequest prepends sha256 to the description
func hashAssistantRequest(aReq *openai.AssistantRequest) error {
	aReqJSON, err := json.Marshal(aReq)
	if err != nil {
		return err
	}

	hashed := sha256.Sum256(aReqJSON)

	metadata := aReq.Metadata

	if metadata == nil {
		metadata = make(map[string]interface{})
		aReq.Metadata = metadata
	}

	metadata["__hash__"] = hex.EncodeToString(hashed[:])

	return nil
}

func (a *App) Run() error {
	args := a.Args
	spew.Dump(args)

	switch {
	case args.Assistant != nil:
		switch {
		case args.Assistant.Create != nil:
			cmd := args.Assistant.Create
			return a.AM.Create(cmd.AssistantFile)
		case args.Assistant.List != nil:
			return a.AM.List()
		case args.Assistant.Use != nil:
			cmd := args.Assistant.Use
			return a.AM.Use(cmd.ID)
		default:
			curid, err := a.AM.CurrentAssistantID()
			if err != nil {
				return err
			}
			fmt.Println("Current Assistant ID:", curid)
		}
	case args.Thread != nil:
		switch {
		case args.Thread.Messages != nil:
			return a.TM.Messages()
		case args.Thread.Use != nil:
			cmd := args.Thread.Use
			return a.TM.Use(cmd.ID)
		default:
			curid, err := a.TM.CurrentThreadID()
			if err != nil {
				return err
			}
			fmt.Println("Current Thread ID:", curid)
		}
	case args.Run != nil:
		cmd := *args.Run
		return a.ThreadRunner.Run(cmd)
	}

	return nil
}

type ThreadRunner struct {
	OAI *openai.Client
	AM  *AssistantManager
}

func (tr *ThreadRunner) Run(cmd RunCmd) error {
	oai := tr.OAI

	assistantID, err := tr.AM.CurrentAssistantID()
	if err != nil {
		return err
	}

	runReq := openai.RunRequest{
		AssistantID: assistantID,
	}

	threadReq := openai.ThreadRequest{
		Messages: []openai.ThreadMessage{
			{
				Role:    openai.ThreadMessageRoleUser,
				Content: cmd.Message,
			},
		},
	}

	req := openai.CreateThreadAndRunRequest{
		RunRequest: runReq,
		Thread:     threadReq,
	}

	res, err := oai.CreateThreadAndRun(context.Background(), req)
	if err != nil {
		return err
	}

	spew.Dump(res)

	return nil
}

type ThreadManager struct {
	OAI    *openai.Client
	JSONDB *JSONDB
}

// Use selects a thread
func (tm *ThreadManager) Use(threadID string) error {
	return tm.JSONDB.Put("currentThread", threadID)
}

// Messages retrieves messages from the current thread
func (tm *ThreadManager) Messages() error {
	threadID, err := tm.CurrentThreadID()
	if err != nil {
		return err
	}

	msgs, err := tm.OAI.ListMessage(context.Background(), threadID, nil, nil, nil, nil)
	if err != nil {
		return err
	}

	spew.Dump(msgs)

	return nil
}

// CurrentThreadID retrieves the current thread ID
func (tm *ThreadManager) CurrentThreadID() (string, error) {
	var threadID string
	ok, err := tm.JSONDB.Get("currentThread", &threadID)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", nil
	}
	return threadID, nil
}

type AssistantManager struct {
	OAI *openai.Client
	// DB  *sqlx.DB
	JSONDB *JSONDB
}

func (am *AssistantManager) List() error {
	as, err := am.OAI.ListAssistants(context.Background(), nil, nil, nil, nil)
	if err != nil {
		return err
	}

	spew.Dump(as)

	return nil
}

func (am *AssistantManager) Create(filePath string) error {
	var aReq openai.AssistantRequest
	err := goo.DecodeFile(filePath, &aReq)
	if err != nil {
		return err
	}

	err = hashAssistantRequest(&aReq)
	if err != nil {
		return err
	}

	oai := am.OAI

	ctx := context.Background()

	assistant, err := oai.CreateAssistant(ctx, aReq)
	if err != nil {
		return err
	}

	spew.Dump(assistant)

	return nil
}

// Use selects an assistant
func (am *AssistantManager) Use(assistantID string) error {
	return am.JSONDB.Put("currentAssistant", assistantID)
}

// CurrentAssistantID
func (am *AssistantManager) CurrentAssistantID() (string, error) {
	var assistantID string
	ok, err := am.JSONDB.Get("currentAssistant", &assistantID)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("no current assistant")
	}
	return assistantID, nil
}

// JSONDB is a key-value store that stores JSON values.
type JSONDB struct {
	DB        *sqlx.DB
	TableName string
}

// Get retrieves a value from the database.
func (db *JSONDB) Get(key string, value interface{}) (bool, error) {
	var jsonValue string
	query := fmt.Sprintf("SELECT value FROM %s WHERE key = ?", db.TableName)
	err := db.DB.Get(&jsonValue, query, key)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, err
	}

	err = json.Unmarshal([]byte(jsonValue), value)
	if err != nil {
		return false, err
	}
	return true, nil
}

// Put upserts a value into the database.
func (db *JSONDB) Put(key string, value interface{}) error {
	jsonValue, err := json.Marshal(value)
	if err != nil {
		return err
	}
	query := fmt.Sprintf("INSERT INTO %s (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value", db.TableName)
	_, err = db.DB.Exec(query, key, string(jsonValue))
	return err
}

// ProvideDB provides a JSONDB instance.
func ProvideJSONDB(db *sqlx.DB) *JSONDB {
	return &JSONDB{
		DB:        db,
		TableName: "keys",
	}
}

// ProvideConfig loads the configuration from the environment.
func ProvideConfig() (*Config, error) {
	return goo.ParseConfig[Config]("")
}

// ProvideArgs parses cli args
func ProvideArgs() (*Args, error) {
	return goo.ParseArgs[Args]()
}

func ProvideOpenAI(cfg *Config) *openai.Client {
	return openai.NewClient(cfg.OpenAI.APIKey)
}

func ProvideGooConfig(cfg *Config) *goo.Config {
	return &cfg.Config
}

var wires = wire.NewSet(
	ProvideGooConfig,
	goo.Wires,
	ProvideConfig,
	ProvideArgs,
	ProvideOpenAI,
	ProvideJSONDB,

	// ProvideLookupDB,
	// ProvideOpenAI,

	wire.Struct(new(ThreadManager), "*"),
	wire.Struct(new(ThreadRunner), "*"),
	wire.Struct(new(AssistantManager), "*"),
	wire.Struct(new(App), "*"),
)
