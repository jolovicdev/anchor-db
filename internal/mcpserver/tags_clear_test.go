package mcpserver

import (
	"encoding/json"
	"testing"
)

// Whether the caller supplied the field is a different question from what it
// contained, and only the first decides between "leave tags alone" and "remove
// them". Collapsing an empty list to nil lost that distinction, so an MCP
// client had no way to clear tags at all.
func TestStringListDistinguishesAbsentFromEmpty(t *testing.T) {
	type payload struct {
		Tags stringList `json:"tags,omitempty"`
	}

	cases := []struct {
		name      string
		json      string
		wantSet   bool
		wantValue []string
	}{
		{"absent", `{}`, false, nil},
		{"empty array", `{"tags":[]}`, true, nil},
		{"empty string", `{"tags":""}`, true, nil},
		{"null", `{"tags":null}`, true, nil},
		{"populated", `{"tags":["a","b"]}`, true, []string{"a", "b"}},
		{"blank entries only", `{"tags":[" ",""]}`, true, nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got payload
			if err := json.Unmarshal([]byte(tc.json), &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if got.Tags.Set != tc.wantSet {
				t.Errorf("Set = %v, want %v", got.Tags.Set, tc.wantSet)
			}
			if len(got.Tags.Values) != len(tc.wantValue) {
				t.Fatalf("Values = %#v, want %#v", got.Tags.Values, tc.wantValue)
			}
			for i := range tc.wantValue {
				if got.Tags.Values[i] != tc.wantValue[i] {
					t.Errorf("Values = %#v, want %#v", got.Tags.Values, tc.wantValue)
				}
			}
		})
	}
}

// The update handler asks for replacement whenever the field was supplied, so
// an empty list must reach the service as "replace with nothing".
func TestEmptyTagsRequestsReplacement(t *testing.T) {
	var input anchorUpdateInput
	if err := json.Unmarshal([]byte(`{"anchor_id":"a","tags":[]}`), &input); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !input.Tags.Set {
		t.Fatal("an empty tags list must be recorded as supplied, or tags can never be cleared")
	}
	if len(input.Tags.Values) != 0 {
		t.Errorf("Values = %#v, want empty", input.Tags.Values)
	}
}
