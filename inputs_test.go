package gpt

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseInput(t *testing.T) {
	tests := []struct {
		input         string
		expectedJSON  string
		expectedError bool
	}{
		{"text:hello world", `{"type":"text","text":"hello world"}`, false},
		{"image:https://example.com/image.jpg", `{"image_url":{"url":"https://example.com/image.jpg"},"type":"image_url"}`, false},
		{"file:testdata/input.md", `{"type":"text","text":"hello from file input\n"}`, false},
		{"file:-", `{"type":"text","text":"hello from stdin"}`, false},
		{"naked input", `{"type":"text","text":"naked input"}`, false},
		{"unknown:considered naked input", `{"type":"text","text":"unknown:considered naked input"}`, false},
	}

	oldStdin := os.Stdin
	defer func() { os.Stdin = oldStdin }()

	tempFile, err := os.CreateTemp("testdata", "stdin-test")
	assert.NoError(t, err)
	defer os.Remove(tempFile.Name())

	_, err = tempFile.WriteString("hello from stdin")
	assert.NoError(t, err)
	_, err = tempFile.Seek(0, 0)
	assert.NoError(t, err)
	os.Stdin = tempFile

	for _, tt := range tests {
		assert := assert.New(t)

		result, err := ParseInput(tt.input)
		if tt.expectedError {
			assert.Error(err, "expected error for input %s but got none", tt.input)
			continue
		}

		assert.NoError(err, "unexpected error for input %s: %v", tt.input, err)

		actualJSON, err := json.Marshal(result)
		assert.NoError(err, "error marshaling result for input %s: %v", tt.input, err)

		assert.JSONEq(tt.expectedJSON, string(actualJSON), "expected JSON %s but got %s for input %s", tt.expectedJSON, string(actualJSON), tt.input)
	}
}
