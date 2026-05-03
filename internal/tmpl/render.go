package tmpl

import (
	"bytes"
	"fmt"
	"os"
	"text/template"
)

// Render loads a Go template file and executes it with data.
// data keys map directly to {{.KeyName}} in the template.
func Render(templatePath string, data map[string]string) (string, error) {
	src, err := os.ReadFile(templatePath)
	if err != nil {
		return "", fmt.Errorf("read template %s: %w", templatePath, err)
	}
	tmpl, err := template.New("prompt").Parse(string(src))
	if err != nil {
		return "", fmt.Errorf("parse template %s: %w", templatePath, err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute template %s: %w", templatePath, err)
	}
	return buf.String(), nil
}
