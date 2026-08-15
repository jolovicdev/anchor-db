package mcpserver

import (
	"encoding/json"
	"reflect"
	"testing"
)

// Hosts disagree about how to encode an array argument. Every one of these
// shapes has an unambiguous meaning, and rejecting them made the tool unusable
// from hosts that flatten arrays into strings.
func TestStringListAcceptsTheShapesHostsSend(t *testing.T) {
	cases := []struct {
		name string
		json string
		want []string
	}{
		{"array", `["auth","retry"]`, []string{"auth", "retry"}},
		{"array delivered as a string", `"[\"auth\",\"retry\"]"`, []string{"auth", "retry"}},
		{"comma-separated string", `"auth,retry"`, []string{"auth", "retry"}},
		{"comma-separated with spaces", `"auth, retry"`, []string{"auth", "retry"}},
		{"single string", `"auth"`, []string{"auth"}},
		{"empty string", `""`, nil},
		{"null", `null`, nil},
		{"empty array", `[]`, nil},
		{"blank entries dropped", `" a , ,b ,"`, []string{"a", "b"}},
		{"whitespace in array entries", `[" auth ","","retry"]`, []string{"auth", "retry"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got stringList
			if err := json.Unmarshal([]byte(tc.json), &got); err != nil {
				t.Fatalf("unmarshal %s: %v", tc.json, err)
			}
			if !reflect.DeepEqual([]string(got), tc.want) {
				t.Errorf("unmarshal %s = %#v, want %#v", tc.json, []string(got), tc.want)
			}
		})
	}
}

// Being permissive about encoding must not extend to accepting values that
// were never a list of tags.
func TestStringListRejectsValuesThatAreNotTags(t *testing.T) {
	for _, input := range []string{`42`, `true`, `{"a":1}`, `[1,2]`} {
		var got stringList
		if err := json.Unmarshal([]byte(input), &got); err == nil {
			t.Errorf("unmarshal %s was accepted as %#v", input, []string(got))
		}
	}
}

// The decoder can only see arguments that pass schema validation first, so the
// advertised schema has to admit the same shapes.
func TestTagsSchemaAdmitsStringsAndArrays(t *testing.T) {
	for name, schema := range map[string]any{
		"anchor_create": toolSchema[createAnchorInput](),
		"anchor_update": toolSchema[anchorUpdateInput](),
	} {
		t.Run(name, func(t *testing.T) {
			encoded, err := json.Marshal(schema)
			if err != nil {
				t.Fatalf("marshal schema: %v", err)
			}
			var doc struct {
				Properties struct {
					Tags struct {
						Type        any    `json:"type"`
						Description string `json:"description"`
					} `json:"tags"`
				} `json:"properties"`
			}
			if err := json.Unmarshal(encoded, &doc); err != nil {
				t.Fatalf("decode schema: %v", err)
			}

			types, ok := doc.Properties.Tags.Type.([]any)
			if !ok {
				t.Fatalf("tags type is %#v, want a list including array and string", doc.Properties.Tags.Type)
			}
			seen := map[string]bool{}
			for _, entry := range types {
				if name, ok := entry.(string); ok {
					seen[name] = true
				}
			}
			for _, want := range []string{"array", "string"} {
				if !seen[want] {
					t.Errorf("tags type %v does not admit %q", types, want)
				}
			}
			// Overriding the type must not cost the field its documentation.
			if doc.Properties.Tags.Description == "" {
				t.Error("tags lost its description")
			}
		})
	}
}
