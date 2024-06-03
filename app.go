package gpt

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"slices"

	"github.com/davecgh/go-spew/spew"
	"github.com/hayeah/goo"
	"github.com/hayeah/goo/fetch"
	"github.com/sashabaranov/go-openai"
)

type App struct {
	Args             *Args
	Config           *Config
	OAI              *openai.Client
	AssistantManager *AssistantManager
	ThreadManager    *ThreadManager
	RunManager       *RunManager
	ThreadRunner     *ThreadRunner
	// Migrate          *migrate.Migrate
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

type ToolCaller interface {
	Exec(call *openai.FunctionCall) (string, error)
}

type CommandCaller struct {
	Program string
}

func (c *CommandCaller) Exec(name, args string) (string, int, error) {
	cmd := exec.Command("sh", "-c", c.Program)
	// cmd := exec.Command("python3", "eval.py")

	// NOTE: env vars are NAME=VALUE strings, where VALUE is a null terminated
	// string. No escape is necessary.
	//
	// See:
	// https://man7.org/linux/man-pages/man7/environ.7.html
	cmd.Env = append(os.Environ(), "TOOL_NAME="+name, "TOOL_ARGS="+args)

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

type OAIClient struct {
	fetch.Client
}
