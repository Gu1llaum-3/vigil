package notifications

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
)

var defaultTitles = map[EventKind]*template.Template{
	EventMonitorDown:  template.Must(template.New("").Parse(`Monitor "{{.Resource.Name}}" is DOWN`)),
	EventMonitorUp:    template.Must(template.New("").Parse(`Monitor "{{.Resource.Name}}" recovered`)),
	EventAgentOffline: template.Must(template.New("").Parse(`Agent "{{.Resource.Name}}" is offline`)),
	EventAgentOnline:  template.Must(template.New("").Parse(`Agent "{{.Resource.Name}}" is back online`)),
	EventContainerImageUpdateAvailable: template.Must(template.New("").Funcs(template.FuncMap{"join": strings.Join}).Parse(`Container image update available for "{{.Resource.Name}}"`)),
}

var defaultBodies = map[EventKind]*template.Template{
	EventMonitorDown: template.Must(template.New("").Parse(
		`Monitor "{{.Resource.Name}}" is DOWN{{if index .Details "last_msg"}} — {{index .Details "last_msg"}}{{end}}`)),
	EventMonitorUp: template.Must(template.New("").Parse(
		`Monitor "{{.Resource.Name}}" recovered (was {{.Previous}})`)),
	EventAgentOffline: template.Must(template.New("").Parse(
		`Agent "{{.Resource.Name}}" went offline`)),
	EventAgentOnline: template.Must(template.New("").Parse(
		`Agent "{{.Resource.Name}}" is back online`)),
	EventContainerImageUpdateAvailable: template.Must(template.New("").Funcs(template.FuncMap{"join": strings.Join}).Parse(
		`Container "{{.Resource.Name}}" on agent "{{index .Details "agent_name"}}" uses {{index .Details "current_ref"}}. Newer versions available{{if index .Details "update_targets"}}: {{join (index .Details "update_targets") ", "}}{{end}}`)),
}

// RenderMessage produces a (title, body) pair for the given event using the default templates.
func RenderMessage(evt Event) (title, body string, err error) {
	title, err = renderTemplate(defaultTitles, evt)
	if err != nil {
		return "", "", fmt.Errorf("render title: %w", err)
	}
	body, err = renderTemplate(defaultBodies, evt)
	if err != nil {
		return "", "", fmt.Errorf("render body: %w", err)
	}
	return title, body, nil
}

func renderTemplate(templates map[EventKind]*template.Template, evt Event) (string, error) {
	tmpl, ok := templates[evt.Kind]
	if !ok {
		return string(evt.Kind), nil
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, evt); err != nil {
		return "", err
	}
	return buf.String(), nil
}
