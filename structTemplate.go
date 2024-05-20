package gpt

import (
	"bytes"
	"encoding/json"
	"html/template"
)

// JSONStructTemplate is a template that renders JSON and unmarshals it into a struct.
type JSONStructTemplate[T any, R any] struct {
	tmpl *template.Template
}

// MustJSONStructTemplate creates a new template panics if there is an error.
func MustJSONStructTemplate[T any, R any](tmplStr string) *JSONStructTemplate[T, R] {
	tmpl, err := NewJSONStructTemplate[T, R](tmplStr)
	if err != nil {
		panic(err)
	}
	return tmpl
}

// NewJSONStructTemplate creates a new template.
func NewJSONStructTemplate[T any, R any](tmplStr string) (*JSONStructTemplate[T, R], error) {
	tmpl, err := template.New("jsonTemplate").Parse(tmplStr)
	if err != nil {
		return nil, err
	}
	return &JSONStructTemplate[T, R]{tmpl: tmpl}, nil
}

// Execute the template with the data.
func (t *JSONStructTemplate[T, R]) Execute(data R) (*T, error) {
	var result T

	var buf bytes.Buffer
	if err := t.tmpl.Execute(&buf, data); err != nil {
		return nil, err
	}

	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		return nil, err
	}

	return &result, nil
}
