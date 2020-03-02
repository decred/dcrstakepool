package helpers

import (
	"bytes"
	"html/template"
)

// Parse parses html templates and returns the template as a string.
func Parse(t *template.Template, name string, data interface{}) (string, error) {
	var doc bytes.Buffer
	err := t.ExecuteTemplate(&doc, name, data)
	if err != nil {
		return "", err
	}
	return doc.String(), nil
}
