package gpt

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"

	"github.com/hayeah/goo/fetch"
	"github.com/sashabaranov/go-openai"
)

type ThreadRunner struct {
	AM *AssistantManager

	oai   *OpenAIV2API
	appDB *AppDB
	log   *slog.Logger
}

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

func (tr *ThreadRunner) RunStream(cmd SendCmdScope) error {
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
		sse, err = ai.SSE("/threads/runs", &fetch.Options{
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
		sse, err = ai.SSE("/threads/{{thread_id}}/runs", &fetch.Options{
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

		// TODO: move submit tool output up here
	}

	if err != nil {
		return err
	}
	defer sse.Close()

	if sse.IsError() {
		return fmt.Errorf("POST /threads/run error: %s", sse.Status)
	}

	f, err := os.Create("stream.sse")
	if err != nil {
		return err
	}
	defer f.Close()

	log := tr.log
	toolw := os.Stderr

processStream:
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
		case "thread.run.step.delta":
			result := event.GJSON(`delta.step_details.tool_calls.#(type==function)#.function`)

			for _, item := range result.Array() {
				switch {
				case item.Get("name").Exists():
					toolw.WriteString("\n")
					log.Info("FunctionCall", "name", item.Get("name"))
				case item.Get("arguments").Exists():
					toolw.WriteString(item.Get("arguments").String())
				}
			}
		case "thread.run.requires_action":
			toolw.WriteString("\n")
			toolw.Sync()

			result := event.GJSON(`required_action.submit_tool_outputs.tool_calls.#(type==function)#`)

			var toolOutputs []openai.ToolOutput

			for _, item := range result.Array() {
				id := item.Get("id").Str // tool call id
				name := item.Get("function.name").Str
				args := item.Get("function.arguments").Str

				log.Info("FunctionCall.Exec",
					"name", name, "cmd", cmd.Tools, "args", args)

				caller := CommandCaller{Program: cmd.Tools}

				output, exitcode, err := caller.Exec(name, args)

				// TODO: print exit status
				if err != nil {
					// TODO submit error to the assistant?
					output = fmt.Sprintf("Execute error: %v\n%s\n", err, output)
				}

				output = fmt.Sprintf("%s\nProgram exit code: %d\n", output, exitcode)

				toolw.WriteString(output)

				// tool_call_id
				// output

				toolOutputs = append(toolOutputs, openai.ToolOutput{
					ToolCallID: id,
					Output:     output,
				})

			}

			// submit tool output

			// RequiresAction is the last event before DONE. Close the previous
			// stream before starting the new tool outputs stream.
			sse.Next() // consume the DONE event, for completion's sake
			sse.Close()

			runID := event.GJSON("id").String()
			threadID := event.GJSON("thread_id").String()

			// https://platform.openai.com/docs/api-reference/runs/submitToolOutputs
			// POST https://api.openai.com/v1/threads/{thread_id}/runs/{run_id}/submit_tool_outputs

			// start a new submit stream
			sse, err = ai.SSE("/threads/{{thread_id}}/runs/{{run_id}}/submit_tool_outputs", &fetch.Options{
				Body: `{
						"tool_outputs": {{tool_outputs}},
						"stream": true,
					  }`,
				BodyParams: map[string]any{
					"tool_outputs": toolOutputs,
				},
				PathParams: map[string]string{
					"thread_id": threadID,
					"run_id":    runID,
				},
			})

			if err != nil {
				return err
			}

			goto processStream

		case "thread.run.step.completed":
			fmt.Print("\n")
			// NB: multipath doesn't work if there are spaces between the commas
			result := event.GJSON("{thread_id,id,usage}")
			fmt.Println(result)
		case "done":
			fmt.Print("\n")
		}
	}

	return sse.Err()
}

type ThreadManager struct {
	db *AppDB
}

// Use selects a thread
func (tm *ThreadManager) Use(threadID string) error {
	return tm.db.PutCurrentThreadID(threadID)
}

// Show retrieves thread info
func (tm *ThreadManager) Show(threadID string) error {
	// var err error
	// if threadID == "" {
	// 	threadID, err = tm.db.CurrentThreadID()
	// 	if err != nil {
	// 		return err
	// 	}

	// }

	// thread, err := tm.OAI.RetrieveThread(context.Background(), threadID)
	// if err != nil {
	// 	return err
	// }

	// goo.PrintJSON(thread)

	return nil
}

// Messages retrieves messages from the current thread
func (tm *ThreadManager) Messages() error {
	// threadID, err := tm.db.CurrentThreadID()
	// if err != nil {
	// 	return err
	// }

	// list, err := tm.OAI.ListMessage(context.Background(), threadID, nil, nil, nil, nil)
	// if err != nil {
	// 	return err
	// }

	// // slices.Reverse()
	// slices.Reverse(list.Messages)

	// for _, msg := range list.Messages {
	// 	spew.Dump(msg.Role)
	// 	for _, content := range msg.Content {
	// 		if content.Text != nil {
	// 			fmt.Print(content.Text.Value)
	// 		}
	// 	}
	// 	fmt.Println()
	// }

	return nil
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
