package gpt

import (
	"github.com/go-resty/resty/v2"
	"github.com/hayeah/goo/fetch"
)

func OpenAIV2(client *resty.Client, secret string) fetch.Client {
	c := fetch.New(client)
	c.SetBaseURL("https://api.openai.com/v1")
	c.SetHeader("Content-Type", "application/json")
	c.SetHeader("OpenAI-Beta", "assistants=v2")
	c.SetAuthToken(secret)

	return c
}
