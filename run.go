package gpt

import (
	"fmt"
	"net/http"

	"github.com/hayeah/goo/fetch"
)

type RunManager struct {
	ai *OpenAIV2API
	db *AppDB
}

type ThreadRunParams struct {
	ThreadID string `json:"thread_id"`
	RunID    string `json:"run_id"`
}

// ThreadRunParams returns the thread ID and run ID.
func (rm *RunManager) ThreadRunParams() (*ThreadRunParams, error) {
	threadID, err := rm.db.CurrentThreadID()
	if err != nil {
		return nil, err
	}

	runID, err := rm.db.CurrentRunID()
	if err != nil {
		return nil, err
	}

	return &ThreadRunParams{
		ThreadID: threadID,
		RunID:    runID,
	}, nil
}

func (rm *RunManager) Show() error {
	oai := rm.ai
	pathParams, err := rm.ThreadRunParams()
	if err != nil {
		return err
	}

	// https://platform.openai.com/docs/api-reference/runs/getRun
	// https://api.openai.com/v1/threads/{thread_id}/runs/{run_id}
	r, err := oai.JSON("/threads/{{ThreadID}}/runs/{{RunID}}", &fetch.Options{
		Method:     http.MethodGet,
		PathParams: pathParams,
	})

	if err != nil {
		return err
	}

	if r.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", r.StatusCode)
	}

	fmt.Println(r)

	return nil
}

func (rm *RunManager) ListSteps() error {
	oai := rm.ai
	pathParams, err := rm.ThreadRunParams()
	if err != nil {
		return err
	}

	// https://platform.openai.com/docs/api-reference/run-steps/listRunSteps
	// https://api.openai.com/v1/threads/{thread_id}/runs/{run_id}/steps
	r, err := oai.JSON("/threads/{{ThreadID}}/runs/{{RunID}}/steps", &fetch.Options{
		Method:     http.MethodGet,
		PathParams: pathParams,
	})

	if err != nil {
		return err
	}

	if r.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", r.StatusCode)
	}

	fmt.Println(r)

	return nil
}
