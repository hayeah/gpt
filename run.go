package gpt

import (
	"context"

	"github.com/hayeah/goo"
	"github.com/sashabaranov/go-openai"
)

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
