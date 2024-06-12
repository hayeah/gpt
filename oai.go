package gpt

import (
	"net/http"

	"github.com/hayeah/goo/fetch"
)

type OpenAIV2API struct {
	fetch.Options
}

func NewOpenAIV2API(secret string) *OpenAIV2API {
	opts := fetch.Options{
		Method:  http.MethodPost,
		BaseURL: "https://api.openai.com/v1",
	}
	opts.SetHeader("Content-Type", "application/json")
	opts.SetHeader("OpenAI-Beta", "assistants=v2")
	opts.SetHeader("Authorization", "Bearer "+secret)

	return &OpenAIV2API{opts}
}
