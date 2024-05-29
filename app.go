package gpt

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"slices"

	"github.com/davecgh/go-spew/spew"
	"github.com/go-resty/resty/v2"
	"github.com/golang-migrate/migrate/v4"
	"github.com/google/wire"
	"github.com/hayeah/go-gpt/oai"
	"github.com/hayeah/goo"
	"github.com/hayeah/goo/fetch"
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

type SendCmdScope struct {
	Inputs         []string `arg:"positional,required"`
	ContinueThread bool     `arg:"--continue,-c" help:"run message using the current thread"`
	Tools          string   `arg:"--tools" help:"process tool use with the given command"`

	// TODO remove
	Message string
}

type ThreadMessagesCmd struct {
}

type ThreadUseCmd struct {
	ID string `arg:"positional,required"`
}

type ThreadCmdScope struct {
	Show     *ThreadShowCmd     `arg:"subcommand:show" help:"show current thread info"`
	Messages *ThreadMessagesCmd `arg:"subcommand:messages" help:"list messages of current thread"`
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

type AssistantCmdScope struct {
	Show   *AssistantShowCmd   `arg:"subcommand:show" help:"show assistant info"`
	List   *AssistantListCmd   `arg:"subcommand:ls" help:"list assistants"`
	Use    *AssistantUseCmd    `arg:"subcommand:use" help:"use assistant"`
	Create *AssistantCreateCmd `arg:"subcommand:create" help:"create assistant"`
}

type RunCmdScope struct {
	Show      *RunShowCmd      `arg:"subcommand:show" help:"show run info"`
	ListSteps *RunListStepsCmd `arg:"subcommand:steps" help:"show steps"`
}

type RunListStepsCmd struct {
}

type RunShowCmd struct {
	ID string `arg:"positional"`
}

type Args struct {
	Assistant *AssistantCmdScope `arg:"subcommand:assistant" help:"manage assistants"`
	Send      *SendCmdScope      `arg:"subcommand:send" help:"run a message in a thread"`
	Thread    *ThreadCmdScope    `arg:"subcommand:thread" help:"manage threads"`
	Run       *RunCmdScope       `arg:"subcommand:run" help:"manage runs"`
}

type App struct {
	Args             *Args
	Config           *Config
	OAI              *openai.Client
	AssistantManager *AssistantManager
	ThreadManager    *ThreadManager
	RunManager       *RunManager
	ThreadRunner     *ThreadRunner
	Migrate          *migrate.Migrate
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
		am := a.AssistantManager
		switch {
		case args.Assistant.Show != nil:
			cmd := args.Assistant.Show
			return am.Show(cmd.ID)
		case args.Assistant.Create != nil:
			cmd := args.Assistant.Create
			return am.Create(cmd.AssistantFile)
		case args.Assistant.List != nil:
			return am.List()
		case args.Assistant.Use != nil:
			cmd := args.Assistant.Use
			return am.Use(cmd.ID)
		default:
			curid, err := a.AssistantManager.CurrentAssistantID()
			if err != nil {
				return err
			}

			return am.Show(curid)
		}
	case args.Thread != nil:
		switch {
		case args.Thread.Show != nil:
			cmd := args.Thread.Show
			return a.ThreadManager.Show(cmd.ThreadID)
		case args.Thread.Messages != nil:
			return a.ThreadManager.Messages()
		case args.Thread.Use != nil:
			cmd := args.Thread.Use
			return a.ThreadManager.Use(cmd.ID)
		}
	case args.Send != nil:
		cmd := *args.Send
		// return a.ThreadRunner.RunStream(cmd)
		return a.ThreadRunner.RunStream2(cmd)
	case args.Run != nil:
		switch {
		case args.Run.Show != nil:
			return a.RunManager.Show()
		case args.Run.ListSteps != nil:
			return a.RunManager.ListSteps()
		default:
			return a.RunManager.Show()
		}
	}

	return nil
}

type AppDB struct {
	jsondb *JSONDB
}

// ProvideAppDB provides an AppDB instance.
func ProvideAppDB(jsondb *JSONDB) *AppDB {
	return &AppDB{jsondb}
}

const (
	keyCurrentThread = "currentThread"
	keyCurrentRun    = "currentRun"
)

// CurrentThreadID retrieves the current thread ID.
func (d *AppDB) CurrentThreadID() (string, error) {
	return d.jsondb.GetString(keyCurrentThread)
}

// PutCurrentThreadID sets the current thread ID.
func (d *AppDB) PutCurrentThreadID(threadID string) error {
	return d.jsondb.Put(keyCurrentThread, threadID)
}

// CurrentRunID retrieves the current run ID.
func (d *AppDB) CurrentRunID() (string, error) {
	return d.jsondb.GetString(keyCurrentRun)
}

// PutCurrentRun sets the current run ID.
func (d *AppDB) PutCurrentRun(id string) error {
	return d.jsondb.Put(keyCurrentRun, id)
}

type ThreadRunner struct {
	OpenAIConfig *OpenAIConfig
	OAI          *openai.Client
	OAIV2        *OpenAIClientV2
	AM           *AssistantManager

	oai *OAIClient

	appDB *AppDB
	log   *slog.Logger
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

func (tr *ThreadRunner) processInputs(inputs []string) ([]json.Marshaler, error) {
	var ms []json.Marshaler
	if len(inputs) == 0 {
		inputs = append(inputs, "-")
	}

	for _, input := range inputs {
		m, err := ParseInput(input)
		if err != nil {
			return nil, err
		}

		ms = append(ms, m)
	}

	return ms, nil
}

func (tr *ThreadRunner) RunStream2(cmd SendCmdScope) error {
	ai := tr.oai

	ms, err := tr.processInputs(cmd.Inputs)
	if err != nil {
		return err
	}

	assistantID, err := tr.AM.CurrentAssistantID()
	if err != nil {
		return err
	}

	var threadID string
	if cmd.ContinueThread {
		threadID, err = tr.appDB.CurrentThreadID()

		if err != nil {
			return err
		}
	}

	var sse *fetch.SSEResponse

	if threadID == "" {
		// https://platform.openai.com/docs/api-reference/runs/createThreadAndRun
		// POST https://api.openai.com/v1/threads/{thread_id}/runs
		sse, err = ai.SSE("POST", "/threads/runs", &fetch.Options{
			Body: `{
				"assistant_id": {{assistantID}},
				"thread": {
				  "messages": [
					{"role": "user", "content": [
						{{#inputs}}
						{{.}},
						{{/inputs}}
					]},
				  ],
				},
				"stream": true,
			}`,
			BodyParams: map[string]any{
				"assistantID": assistantID,
				"inputs":      ms,
			},
		})
	} else {
		// https://platform.openai.com/docs/api-reference/runs/createRun
		// POST https://api.openai.com/v1/threads/{thread_id}/runs
		sse, err = ai.SSE("POST", "/threads/{thread_id}/runs", &fetch.Options{
			Body: `{
				"assistant_id": {{assistantID}},

				"additional_messages": [
					{"role": "user", "content": [
						{{#inputs}}
						{{.}},
						{{/inputs}}
					]},
				],

				"stream": true,
			}`,
			BodyParams: map[string]any{
				"assistantID": assistantID,
				"inputs":      ms,
			},
			PathParams: map[string]string{
				"thread_id": threadID,
			},
		})
	}

	if err != nil {
		return err
	}
	defer sse.Close()

	if sse.IsError() {
		return fmt.Errorf("POST /threads/run error: %s\n%s", sse.Status(), sse.String())
	}

	f, err := os.Create("stream.sse")
	if err != nil {
		return err
	}
	defer f.Close()

	sse.Tee(f)

	for sse.Next() {
		event := sse.Event()

		switch event.Event {
		case "thread.created":
			id := event.GJSON("id").String()
			err = tr.appDB.PutCurrentThreadID(id)
			if err != nil {
				return err
			}
		case "thread.run.created":
			id := event.GJSON("id").String()
			err = tr.appDB.PutCurrentRun(id)
			if err != nil {
				return err
			}
		case "thread.message.delta":
			result := event.GJSON("delta.content.#.text.value")
			for _, item := range result.Array() {
				fmt.Print(item.String())
			}
		}
	}

	fmt.Print("\n")

	return sse.Err()
}

func (tr *ThreadRunner) RunStream(cmd SendCmdScope) error {

	oa := tr.OAIV2
	ctx := context.Background()

	log := tr.log

	assistantID, err := tr.AM.CurrentAssistantID()
	if err != nil {
		return err
	}

	var threadID string
	if cmd.ContinueThread {
		threadID, err = tr.appDB.CurrentThreadID()

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

	outf, err := os.Create("stream.sse")
	if err != nil {
		return err
	}
	defer outf.Close()

processStream:
	stream.TeeSSE(outf)

	toolw := os.Stderr

	for stream.Next() {
		// process text delta
		text, ok := stream.MessageDeltaText()
		if ok {
			fmt.Fprint(os.Stdout, text)
			// fmt.Println(text)
			continue
		}

		// process everything else

		event := stream.Event()
		switch event := event.(type) {
		case *openai.StreamThreadCreated:
			err = tr.appDB.PutCurrentThreadID(event.Thread.ID)
			if err != nil {
				return err
			}
		case *openai.StreamThreadRunCreated:
			err = tr.appDB.PutCurrentRun(event.Run.ID)
			if err != nil {
				return err
			}
		case *openai.StreamRunStepDelta:
			for _, tc := range event.RunStepDelta.Delta.StepDetails.ToolCalls {
				switch {
				case tc.Function.Name != "":
					toolw.WriteString("\n")
					log.Info("FunctionCall", "name", tc.Function.Name)
				case tc.Function.Arguments != "":
					toolw.WriteString(tc.Function.Arguments)
				}

				// code, err := PartialDecodeCodeArguments(buf.Bytes())
				// if err == nil {
				// 	newCode := code[len(lastCode):]
				// 	lastCode = code
				// 	fmt.Print(newCode)
				// }
			}

		case *openai.StreamThreadRunRequiresAction:
			toolw.WriteString("\n")
			toolw.Sync()

			if cmd.Tools == "" {
				return fmt.Errorf("--tools is required to handle tool calls")
			}

			var toolOutputs []openai.ToolOutput
			for _, tc := range event.Run.RequiredAction.SubmitToolOutputs.ToolCalls {
				caller := CommandCaller{Program: cmd.Tools}

				log.Info("FunctionCall.Exec", "name", tc.Function.Name, "cmd", cmd.Tools)

				output, exitcode, err := caller.Exec(&tc.Function)

				// TODO: print exit status
				if err != nil {
					// TODO submit error to the assistant?
					output = fmt.Sprintf("Execute error: %v\n%s\n", err, output)
				}

				output = fmt.Sprintf("%s\nProgram exit code: %d\n", output, exitcode)

				toolw.WriteString(output)

				toolOutputs = append(toolOutputs, openai.ToolOutput{
					ToolCallID: tc.ID,
					Output:     output,
				})
			}

			submitOutputs := openai.SubmitToolOutputsRequest{
				ToolOutputs: toolOutputs,
			}

			// RequiresAction is the last event before DONE. Close the previous
			// stream before starting the new tool outputs stream.
			stream.Next() // consume the DONE event, for completion's sake
			stream.Close()

			// start a new submit stream
			stream, err = oa.SubmitToolOutputsStream(ctx, event.ThreadID, event.Run.ID, submitOutputs)
			if err != nil {
				return err
			}

			goto processStream
		case *openai.StreamThreadRunCompleted:
			// TODO: print tokens usage
			fmt.Println("")
		}
	}

	return err
}

type ToolCaller interface {
	Exec(call *openai.FunctionCall) (string, error)
}

type CommandCaller struct {
	Program string
}

func (c *CommandCaller) Exec(call *openai.FunctionCall) (string, int, error) {
	cmd := exec.Command("sh", "-c", c.Program)
	// cmd := exec.Command("python3", "eval.py")

	// NOTE: env vars are NAME=VALUE strings, where VALUE is a null terminated
	// string. No escape is necessary.
	//
	// See:
	// https://man7.org/linux/man-pages/man7/environ.7.html
	cmd.Env = append(os.Environ(), "TOOL_NAME="+call.Name, "TOOL_ARGS="+call.Arguments)

	out, err := cmd.CombinedOutput()
	exitCode := cmd.ProcessState.ExitCode()

	return string(out), exitCode, err

}

// func handleFunctionCall(call *openai.FunctionCall) (string, error) {
// 	fmt.Println("Function:", call.Name)
// 	fmt.Println("Arguments:", call.Arguments)

// 	return "1.7724538509055159", nil
// }

type RunManager struct {
	oai *OpenAIClientV2
	db  *AppDB
}

func (rm *RunManager) Show() error {
	threadID, err := rm.db.CurrentThreadID()
	if err != nil {
		return err
	}

	runID, err := rm.db.CurrentRunID()
	if err != nil {
		return err
	}

	run, err := rm.oai.RetrieveRun(context.Background(), threadID, runID)
	if err != nil {
		return err
	}

	goo.PrintJSON(run)

	return nil
}

func (rm *RunManager) ListSteps() error {
	threadID, err := rm.db.CurrentThreadID()
	if err != nil {
		return err
	}

	runID, err := rm.db.CurrentRunID()
	if err != nil {
		return err
	}

	steps, err := rm.oai.ListRunSteps(context.Background(), threadID, runID, openai.Pagination{})
	if err != nil {
		return err
	}

	goo.PrintJSON(steps)

	return nil
}

type ThreadManager struct {
	OAI *OpenAIClientV2
	db  *AppDB
}

// Use selects a thread
func (tm *ThreadManager) Use(threadID string) error {
	return tm.db.PutCurrentThreadID(threadID)
}

// Show retrieves thread info
func (tm *ThreadManager) Show(threadID string) error {
	var err error
	if threadID == "" {
		threadID, err = tm.db.CurrentThreadID()
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
	threadID, err := tm.db.CurrentThreadID()
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

	goo.PrintJSON(as)

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

	goo.PrintJSON(assistant)

	return am.JSONDB.Put("currentAssistant", assistant.ID)
	// return nil
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

func (db *JSONDB) GetString(key string) (string, error) {
	var value string
	ok, err := db.Get(key, &value)
	if err != nil {
		return "", err
	}

	if !ok {
		return "", nil
	}

	return value, nil

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

type OAIClient struct {
	fetch.Client
}

func ProvideOAI(cfg *Config) *OAIClient {
	client := resty.New()
	return &OAIClient{oai.V2(client, cfg.OpenAI.APIKey)}
}

func ProvideGooConfig(cfg *Config) *goo.Config {
	return &cfg.Config
}

func ProvideSlog() *slog.Logger {
	// logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	return slog.Default()

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
	ProvideAppDB,
	ProvideSlog,
	ProvideOAI,

	// ProvideLookupDB,
	// ProvideOpenAI,

	wire.Struct(new(ThreadManager), "*"),
	wire.Struct(new(ThreadRunner), "*"),
	wire.Struct(new(AssistantManager), "*"),
	wire.Struct(new(RunManager), "*"),
	wire.Struct(new(App), "*"),
)
