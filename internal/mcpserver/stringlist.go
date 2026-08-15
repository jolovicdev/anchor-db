package mcpserver

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
)

// schemaAcceptingStringLists infers the schema for an input type, then widens
// every stringList in it to accept a string as well as an array.
//
// Tolerating the string forms when decoding is not enough on its own: the SDK
// validates arguments against the advertised schema first, so a flattened tags
// argument is rejected before UnmarshalJSON ever runs. The schema has to admit
// what the decoder is prepared to accept.
func schemaAcceptingStringLists[T any]() *jsonschema.Schema {
	schema, err := jsonschema.For[T](&jsonschema.ForOptions{
		TypeSchemas: map[reflect.Type]*jsonschema.Schema{
			reflect.TypeFor[stringList](): {
				Types: []string{"array", "string", "null"},
				Items: &jsonschema.Schema{Type: "string"},
			},
		},
	})
	if err != nil {
		// The input types are compile-time constants, so this cannot fail for a
		// reason that a caller could do anything about.
		panic(fmt.Sprintf("infer schema for %T: %v", *new(T), err))
	}
	return schema
}

// stringList is a []string that also accepts the shapes hosts actually send.
//
// Tool arguments cross a boundary that not every host encodes the same way:
// some deliver an array argument as a JSON string, so a correctly formed tags
// value arrives as `"[\"auth\",\"retry\"]"` and fails to decode into []string.
// The caller's intent is not ambiguous in that case, and rejecting it only
// makes the tool unusable from those hosts, so the string forms are accepted
// and normalized here.
type stringList []string

// UnmarshalJSON accepts a JSON array of strings, a JSON array delivered as a
// string, or a plain comma-separated string.
func (l *stringList) UnmarshalJSON(data []byte) error {
	// The correct shape first, so nothing changes for hosts that send it.
	var items []string
	if err := json.Unmarshal(data, &items); err == nil {
		*l = cleanStrings(items)
		return nil
	}

	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("tags must be an array of strings, or a string holding "+
			"a JSON array or a comma-separated list, got %s", firstRunes(string(data), 40))
	}

	raw = strings.TrimSpace(raw)
	if raw == "" {
		*l = nil
		return nil
	}

	// An array that made the trip as a string.
	if strings.HasPrefix(raw, "[") {
		var nested []string
		if err := json.Unmarshal([]byte(raw), &nested); err == nil {
			*l = cleanStrings(nested)
			return nil
		}
	}

	*l = cleanStrings(strings.Split(raw, ","))
	return nil
}

// cleanStrings trims each entry and drops the empty ones, so "a, b," and
// ["a"," b",""] both land as ["a","b"] rather than carrying blank tags that
// nothing can match on.
func cleanStrings(items []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func firstRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "..."
}
