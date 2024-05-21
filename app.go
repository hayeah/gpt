package gpt

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"

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
	Message        string `arg:"positional,required"`
	ContinueThread bool   `arg:"--continue,-c" help:"run message using the current thread"`
}

type ThreadMessagesCmd struct {
}

type ThreadUseCmd struct {
	ID string `arg:"positional,required"`
}

type ThreadScopeCmd struct {
	Show     *ThreadShowCmd     `arg:"subcommand:show" help:"show thread info"`
	Messages *ThreadMessagesCmd `arg:"subcommand:messages" help:"list messages"`
	Use      *ThreadUseCmd      `arg:"subcommand:use" help:"use thread"`
}

type ThreadShowCmd struct {
	ThreadID string `arg:"positional"`
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

type AssistantShowCmd struct {
	ID string `arg:"positional"`
}

type AssistantScopeCmd struct {
	Show   *AssistantShowCmd   `arg:"subcommand:show" help:"show assistant info"`
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

	switch {
	case args.Assistant != nil:
		switch {
		case args.Assistant.Show != nil:
			cmd := args.Assistant.Show
			return a.AM.Show(cmd.ID)
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
		case args.Thread.Show != nil:
			cmd := args.Thread.Show
			return a.TM.Show(cmd.ThreadID)
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
		return a.ThreadRunner.RunStream(cmd)
	}

	return nil
}

type ThreadRunner struct {
	OpenAIConfig *OpenAIConfig
	OAI          *openai.Client
	OAIV2        *OpenAIClientV2
	AM           *AssistantManager
	JSONDB       *JSONDB
}

type createRunRequest struct {
	AssistantID string
	Message     string
}

var createThreadAndRunTemplate = MustJSONStructTemplate[openai.CreateThreadAndRunRequest, createRunRequest](`{
	"assistant_id": "{{.AssistantID}}",
	"thread": {
		"messages": [
			{"role": "user", "content": "{{.Message}}"}
		]
	}
}`)

var createRunTemplate = MustJSONStructTemplate[openai.RunRequest, createRunRequest](`{
	"assistant_id": "{{.AssistantID}}",
	"additional_messages": [
		{"role": "user", "content": "{{.Message}}"}
	]
}`)

func (tr *ThreadRunner) RunStream(cmd RunCmd) error {
	oa := tr.OAIV2
	ctx := context.Background()

	assistantID, err := tr.AM.CurrentAssistantID()
	if err != nil {
		return err
	}

	var threadID string
	if cmd.ContinueThread {
		_, err := tr.JSONDB.Get("currentThread", &threadID)

		if err != nil {
			return err
		}
	}

	var stream *openai.StreamerV2
	if threadID != "" {
		runReq, err := createRunTemplate.Execute(createRunRequest{
			AssistantID: assistantID,
			Message:     cmd.Message,
		})
		if err != nil {
			return err
		}

		stream, err = oa.CreateRunStream(ctx, threadID, *runReq)
		if err != nil {
			return err
		}
	} else {
		// 	runReq := openai.RunRequest{
		// 		AssistantID: assistantID,
		// 	}

		// 	threadReq := openai.ThreadRequest{
		// 		Messages: []openai.ThreadMessage{
		// 			{
		// 				Role:    openai.ThreadMessageRoleUser,
		// 				Content: cmd.Message,
		// 			},
		// 		},
		// 	}

		// 	req := openai.CreateThreadAndRunRequest{
		// 		RunRequest: runReq,
		// 		Thread:     threadReq,
		// 	}

		threadRunReq, err := createThreadAndRunTemplate.Execute(createRunRequest{
			AssistantID: assistantID,
			Message:     cmd.Message,
		})
		if err != nil {
			return err
		}

		stream, err = oa.CreateThreadAndRunStream(ctx, *threadRunReq)
		if err != nil {
			return err
		}
	}

	defer stream.Close()

loop:
	for stream.Next() {
		event := stream.Event()

		switch event := event.(type) {
		case *openai.StreamThreadCreated:
			spew.Dump(event.ID)
			tr.JSONDB.Put("currentThread", event.Thread.ID)
		case *openai.StreamThreadRunCreated:
			spew.Dump(event.ID)
			break loop
		}
	}

	_, err = io.Copy(os.Stdout, stream)
	return err
}

type ThreadManager struct {
	OAI    *OpenAIClientV2
	JSONDB *JSONDB
}

// Use selects a thread
func (tm *ThreadManager) Use(threadID string) error {
	return tm.JSONDB.Put("currentThread", threadID)
}

// Show retrieves thread info
func (tm *ThreadManager) Show(threadID string) error {
	var err error
	if threadID == "" {
		threadID, err = tm.CurrentThreadID()
		if err != nil {
			return err
		}

	}

	thread, err := tm.OAI.RetrieveThread(context.Background(), threadID)
	if err != nil {
		return err
	}

	goo.PrintJSON(thread)

	return nil
}

// Messages retrieves messages from the current thread
func (tm *ThreadManager) Messages() error {
	threadID, err := tm.CurrentThreadID()
	if err != nil {
		return err
	}

	list, err := tm.OAI.ListMessage(context.Background(), threadID, nil, nil, nil, nil)
	if err != nil {
		return err
	}

	// slices.Reverse()
	slices.Reverse(list.Messages)

	for _, msg := range list.Messages {
		spew.Dump(msg.Role)
		for _, content := range msg.Content {
			if content.Text != nil {
				fmt.Print(content.Text.Value)
			}
		}
		fmt.Println()
	}

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
	OAI    *OpenAIClientV2
	JSONDB *JSONDB
}

// Show retrieves assistant info
func (am *AssistantManager) Show(assistantID string) error {
	var err error
	if assistantID == "" {
		assistantID, err = am.CurrentAssistantID()
		if err != nil {
			return err
		}
	}

	assistant, err := am.OAI.RetrieveAssistant(context.Background(), assistantID)
	if err != nil {
		return err
	}

	goo.PrintJSON(assistant)

	return nil

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

	// NOTE: the yaml is parsing into any correctly, but th mapping from that to
	// openai.AssistantRequest is not working because of its custom UnmarshalJSON

	// fileData, err := os.ReadFile(filePath)
	// if err != nil {
	// 	return err
	// }
	// var yamlObj interface{}
	// err = yaml.Unmarshal(fileData, &yamlObj)
	// if err != nil {
	// 	return err
	// }
	// spew.Dump(yamlObj)

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

	goo.PrintJSON(assistant)

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

func ProvideOpenAIConfig(cfg *Config) *OpenAIConfig {
	return &cfg.OpenAI
}

type OpenAIClientV2 struct {
	openai.Client
}

func ProvideOpenAIV2(cfg *Config) *OpenAIClientV2 {
	ocfg := openai.DefaultConfig(cfg.OpenAI.APIKey)
	ocfg.AssistantVersion = "v2"
	oa := openai.NewClientWithConfig(ocfg)

	c := OpenAIClientV2{*oa}

	return &c
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
	ProvideOpenAIV2,
	ProvideOpenAIConfig,
	ProvideJSONDB,

	// ProvideLookupDB,
	// ProvideOpenAI,

	wire.Struct(new(ThreadManager), "*"),
	wire.Struct(new(ThreadRunner), "*"),
	wire.Struct(new(AssistantManager), "*"),
	wire.Struct(new(App), "*"),
)
