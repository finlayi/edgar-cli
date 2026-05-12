package edgar

import (
	"encoding/json"
	"fmt"
	"io"
	"time"
)

type Envelope struct {
	OK       bool           `json:"ok"`
	Command  string         `json:"command"`
	Provider string         `json:"provider"`
	Data     any            `json:"data"`
	Error    *EnvelopeError `json:"error"`
	Meta     map[string]any `json:"meta"`
}

type EnvelopeError struct {
	Code      ErrorCode `json:"code"`
	Message   string    `json:"message"`
	Retriable bool      `json:"retriable"`
}

type CommandResult struct {
	Data        any
	MetaUpdates map[string]any
}

func timestampNow() string {
	return time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
}

func successEnvelope(command string, data any, view string, metaUpdates map[string]any) Envelope {
	meta := map[string]any{
		"timestamp":     timestampNow(),
		"output_schema": "v1",
		"view":          view,
	}
	for key, value := range metaUpdates {
		meta[key] = value
	}
	return Envelope{
		OK:       true,
		Command:  command,
		Provider: "sec",
		Data:     data,
		Error:    nil,
		Meta:     meta,
	}
}

func failureEnvelope(command string, cliErr *CLIError, view string) Envelope {
	return Envelope{
		OK:       false,
		Command:  command,
		Provider: "sec",
		Data:     nil,
		Error: &EnvelopeError{
			Code:      cliErr.Code,
			Message:   cliErr.Message,
			Retriable: cliErr.Retriable,
		},
		Meta: map[string]any{
			"timestamp":     timestampNow(),
			"output_schema": "v1",
			"view":          view,
		},
	}
}

func writeJSONLine(w io.Writer, payload any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "%s\n", encoded)
	return err
}

func writePrettyJSONLine(w io.Writer, payload any) error {
	encoded, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "%s\n", encoded)
	return err
}
