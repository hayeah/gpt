package gpt

import (
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"strings"
)

type InputText struct {
	Text string
}

type InputImageURL struct {
	URL    url.URL
	Detail string // "low" | "high" | "auto"
}

// Implementing the MarshalJSON method for InputText
func (it *InputText) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]string{
		"type": "text",
		"text": it.Text,
	})
}

// Implementing the MarshalJSON method for InputImageURL
func (iu *InputImageURL) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]string{
		"type":   "image_url",
		"url":    iu.URL.String(),
		"detail": iu.Detail,
	})
}

func ReadInputFile(path string) (*InputText, error) {
	// FIXME: handle case where "-"  is used twice?
	if path == "-" {
		content, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, errors.New("error reading from stdin")
		}
		return &InputText{Text: string(content)}, nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.New("error reading file")
	}
	return &InputText{Text: string(content)}, nil
}

func ParseNakedInput(input string) (json.Marshaler, error) {
	isStdin := input == "-"
	if isStdin {
		return ReadInputFile("-")
	}

	if _, err := os.Stat(input); err == nil {
		return ReadInputFile(input)
	}

	return &InputText{Text: input}, nil
}

func ParseInput(input string) (json.Marshaler, error) {
	parts := strings.SplitN(input, ":", 2)
	if len(parts) != 2 {
		return ParseNakedInput(input)
	}

	switch parts[0] {
	case "text":
		return &InputText{Text: parts[1]}, nil
	case "image":
		parsedURL, err := url.Parse(parts[1])
		if err != nil {
			return nil, errors.New("invalid URL format")
		}
		return &InputImageURL{URL: *parsedURL, Detail: "auto"}, nil
	case "file":
		return ReadInputFile(parts[1])
	default:
		return ParseNakedInput(input)
	}
}
