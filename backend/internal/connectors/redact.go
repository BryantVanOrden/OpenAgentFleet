package connectors

import (
	"errors"
	"strings"
)

// redactKey scrubs a credential out of an error before it escapes the connector.
//
// Connector errors do not stay in the log. They are written to the task's
// `error` column, broadcast on the WebSocket event bus, and pushed to the
// operator's phone as an alert body. Anything a provider echoes back — a URL, a
// request dump, a helpful "invalid key: sk-..." message — ends up in all of
// them and is retained.
//
// This is defence in depth, not the primary control: keys belong in headers,
// which is where every connector now puts them. This catches the case where a
// provider quotes the key back at us in its own error text.
func redactKey(err error, key string) error {
	if err == nil || len(key) < 8 {
		return err
	}
	msg := err.Error()
	if !strings.Contains(msg, key) {
		return err
	}
	return errors.New(strings.ReplaceAll(msg, key, "[redacted]"))
}
