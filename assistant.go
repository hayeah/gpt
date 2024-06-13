package gpt

import (
	"fmt"

	"github.com/hayeah/goo"
	"github.com/hayeah/goo/fetch"
)

type AssistantManager struct {
	oai    *OpenAIV2API
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

	oai := am.oai
	// https://platform.openai.com/docs/api-reference/assistants/getAssistant
	// GET https://api.openai.com/v1/assistants/{assistant_id}

	r, err := oai.JSON("GET", "/assistants/{{.}}", &fetch.Options{
		PathParams: assistantID,
	})

	if err != nil {
		return err
	}

	fmt.Println(r)

	return nil

}

func (am *AssistantManager) List() error {
	// https://platform.openai.com/docs/api-reference/assistants/listAssistants
	// GET https://api.openai.com/v1/assistants

	oai := am.oai
	r, err := oai.JSON("GET", "/assistants", nil)

	if err != nil {
		return err
	}

	fmt.Println(r)

	return nil
}

func (am *AssistantManager) Create(dataURL string) error {
	var assistantRequest any
	err := goo.DecodeURL(dataURL, &assistantRequest)
	if err != nil {
		return err
	}

	oai := am.oai

	// POST https://api.openai.com/v1/assistants
	r, err := oai.JSON("POST", "/assistants", &fetch.Options{
		Body: assistantRequest,
	})

	if err != nil {
		return err
	}

	fmt.Println(r)

	assistantID := r.Get("id").String()
	if assistantID != "" {
		am.JSONDB.Put("currentAssistant", assistantID)
	}

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

// hashAssistantRequest prepends sha256 to the description
// func hashAssistantRequest(aReq *openai.AssistantRequest) error {
// 	aReqJSON, err := json.Marshal(aReq)
// 	if err != nil {
// 		return err
// 	}

// 	hashed := sha256.Sum256(aReqJSON)

// 	metadata := aReq.Metadata

// 	if metadata == nil {
// 		metadata = make(map[string]interface{})
// 		aReq.Metadata = metadata
// 	}

// 	metadata["__hash__"] = hex.EncodeToString(hashed[:])

// 	return nil
// }
