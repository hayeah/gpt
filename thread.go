package gpt

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"

	"github.com/hayeah/goo/fetch"
	"github.com/sashabaranov/go-openai"
)

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

		// TODO: move submit tool output up here
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
			sse, err = ai.SSE("POST", "/threads/{thread_id}/runs/{run_id}/submit_tool_outputs", &fetch.Options{
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

				name := tc.Function.Name
				args := tc.Function.Arguments

				output, exitcode, err := caller.Exec(name, args)

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
