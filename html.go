package main

import (
	_ "embed"
	"fmt"
	"html/template"
	"io"
	"path/filepath"
)

//go:embed web/viewer.html
var viewerHTML string

type viewerData struct {
	Title  string
	Source string
	Trace  []Step
	Output string
}

func RenderHTML(w io.Writer, sourcePath string, trace *Trace) error {
	tmpl, err := template.New("viewer").Parse(viewerHTML)
	if err != nil {
		return fmt.Errorf("parse viewer template: %w", err)
	}

	data := viewerData{
		Title:  filepath.Base(sourcePath),
		Source: trace.Source,
		Trace:  trace.Steps,
		Output: trace.Output,
	}

	if err := tmpl.Execute(w, data); err != nil {
		return fmt.Errorf("render viewer: %w", err)
	}

	return nil
}
