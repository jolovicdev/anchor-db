package mcpserver

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
)

// nullableInt is an optional integer that also accepts the numeric strings
// hosts send when they flatten arguments.
//
// This is the same hazard stringList covers, and it reaches every parameter
// whose schema is a union with null: an omitted-or-index argument like
// `candidate` is *int, which advertises ["null","integer"], and a host sending
// "0" was rejected before the handler ran. Nothing about "0" is ambiguous.
type nullableInt struct {
	Value int
	Set   bool
}

func (n *nullableInt) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*n = nullableInt{}
		return nil
	}

	var number int
	if err := json.Unmarshal(data, &number); err == nil {
		*n = nullableInt{Value: number, Set: true}
		return nil
	}

	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("expected an integer or a string holding one, got %s", firstRunes(string(data), 40))
	}
	if raw = strings.TrimSpace(raw); raw == "" {
		*n = nullableInt{}
		return nil
	}
	number, err := strconv.Atoi(raw)
	if err != nil {
		return fmt.Errorf("expected an integer, got %q", raw)
	}
	*n = nullableInt{Value: number, Set: true}
	return nil
}

// Ptr converts back to the optional integer the service layer expects.
func (n nullableInt) Ptr() *int {
	if !n.Set {
		return nil
	}
	value := n.Value
	return &value
}

// toolSchema infers the schema for an input type, then widens the types that
// accept more shapes than reflection can express.
//
// Tolerating those shapes when decoding is not enough on its own: the SDK
// validates arguments against the advertised schema first, so a flattened
// argument is rejected before UnmarshalJSON ever runs. The schema has to admit
// what the decoder is prepared to accept.
func toolSchema[T any]() *jsonschema.Schema {
	schema, err := jsonschema.For[T](&jsonschema.ForOptions{
		TypeSchemas: map[reflect.Type]*jsonschema.Schema{
			reflect.TypeFor[stringList](): {
				Types: []string{"array", "string", "null"},
				Items: &jsonschema.Schema{Type: "string"},
			},
			reflect.TypeFor[nullableInt](): {
				Types: []string{"integer", "string", "null"},
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

// emptyIfNil keeps list-shaped fields encoding as [] rather than null, so a
// client never has to null-check a field that is always a list.
func emptyIfNil[T any](items []T) []T {
	if items == nil {
		return []T{}
	}
	return items
}

// stringList is a []string that also accepts the shapes hosts actually send.
//
// Tool arguments cross a boundary that not every host encodes the same way:
// some deliver an array argument as a JSON string, so a correctly formed tags
// value arrives as `"[\"auth\",\"retry\"]"` and fails to decode into []string.
// The caller's intent is not ambiguous in that case, and rejecting it only
// makes the tool unusable from those hosts, so the string forms are accepted
// and normalized here.
type stringList struct {
	Values []string
	// Set records that the caller supplied the field at all, which is what
	// separates "leave the tags alone" from "remove every tag". Collapsing an
	// empty list to nil lost that distinction, so tags could never be cleared.
	Set bool
}

// UnmarshalJSON accepts a JSON array of strings, a JSON array delivered as a
// string, or a plain comma-separated string.
func (l *stringList) UnmarshalJSON(data []byte) error {
	// The correct shape first, so nothing changes for hosts that send it.
	var items []string
	if err := json.Unmarshal(data, &items); err == nil {
		*l = stringList{Values: cleanStrings(items), Set: true}
		return nil
	}

	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("tags must be an array of strings, or a string holding "+
			"a JSON array or a comma-separated list, got %s", firstRunes(string(data), 40))
	}

	raw = strings.TrimSpace(raw)
	if raw == "" {
		*l = stringList{Set: true}
		return nil
	}

	// An array that made the trip as a string.
	if strings.HasPrefix(raw, "[") {
		var nested []string
		if err := json.Unmarshal([]byte(raw), &nested); err == nil {
			*l = stringList{Values: cleanStrings(nested), Set: true}
			return nil
		}
	}

	*l = stringList{Values: cleanStrings(strings.Split(raw, ",")), Set: true}
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

// MarshalJSON keeps the wire shape a plain array, so the wrapper is invisible
// to anything reading these structures back.
func (l stringList) MarshalJSON() ([]byte, error) {
	if l.Values == nil {
		return []byte("null"), nil
	}
	return json.Marshal(l.Values)
}
