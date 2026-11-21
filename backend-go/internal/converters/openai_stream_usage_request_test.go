package converters

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/soulteary/yunqi/internal/session"
	"github.com/soulteary/yunqi/internal/types"
)

func TestOpenAIChatConverterStreamUsageOptions(t *testing.T) {
	for _, tc := range []struct {
		name    string
		stream  bool
		options interface{}
		want    interface{}
	}{
		{"stream_default", true, nil, map[string]interface{}{"include_usage": true}},
		{"stream_other_options", true, map[string]interface{}{"include_obfuscation": false}, map[string]interface{}{"include_obfuscation": false, "include_usage": true}},
		{"explicit_opt_out", true, map[string]interface{}{"include_usage": false}, map[string]interface{}{"include_usage": false}},
		{"explicit_opt_in", true, map[string]interface{}{"include_usage": true}, map[string]interface{}{"include_usage": true}},
		{"nonstream_default", false, nil, nil},
		{"nonstream_options", false, map[string]interface{}{"include_usage": false}, map[string]interface{}{"include_usage": false}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before, _ := json.Marshal(tc.options)
			request := &types.ResponsesRequest{Model: "local", Stream: tc.stream, Input: "hello", StreamOptions: tc.options}
			converted, err := (&OpenAIChatConverter{}).ToProviderRequest(&session.Session{}, request)
			if err != nil {
				t.Fatal(err)
			}
			actual := converted.(map[string]interface{})["stream_options"]
			if !reflect.DeepEqual(actual, tc.want) {
				t.Fatalf("options=%#v, want %#v", actual, tc.want)
			}
			after, _ := json.Marshal(tc.options)
			if string(before) != string(after) {
				t.Fatalf("mutated source options: %s -> %s", before, after)
			}
		})
	}
}
