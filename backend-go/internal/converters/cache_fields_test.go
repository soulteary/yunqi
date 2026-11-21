package converters

import (
	"encoding/json"
	"github.com/soulteary/yunqi/internal/session"
	"github.com/soulteary/yunqi/internal/types"
	"github.com/tidwall/gjson"
	"testing"
)

func TestOpenAICacheFieldsRoundTrip(t *testing.T) {
	for _, value := range []string{`null`, `false`, `0`, `{"prewarm":false,"budget":0}`, `"24h"`} {
		t.Run(value, func(t *testing.T) {
			raw := []byte(`{"model":"m","messages":[],"prompt_cache_key":` + value + `,"prompt_cache_options":` + value + `,"prompt_cache_retention":` + value + `}`)
			responses := ConvertChatRequestToResponsesRequest(raw)
			var req types.ResponsesRequest
			if err := json.Unmarshal(responses, &req); err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(req)
			if err != nil {
				t.Fatal(err)
			}
			chat := ConvertResponsesToOpenAIChatRequest("m", encoded, false)
			converted, err := (&OpenAIChatConverter{}).ToProviderRequest(&session.Session{}, &req)
			if err != nil {
				t.Fatal(err)
			}
			provider, err := json.Marshal(converted)
			if err != nil {
				t.Fatal(err)
			}
			passthrough, err := (&ResponsesPassthroughConverter{}).ToProviderRequest(&session.Session{}, &req)
			if err != nil {
				t.Fatal(err)
			}
			direct, err := json.Marshal(passthrough)
			if err != nil {
				t.Fatal(err)
			}
			for _, body := range [][]byte{responses, encoded, chat, provider, direct} {
				for _, key := range []string{"prompt_cache_key", "prompt_cache_options", "prompt_cache_retention"} {
					if got := gjson.GetBytes(body, key).Raw; got != value {
						t.Fatalf("%s: got %q want %q in %s", key, got, value, body)
					}
				}
			}
		})
	}
}

func TestOpenAICacheFieldsAbsent(t *testing.T) {
	response := ConvertChatRequestToResponsesRequest([]byte(`{"messages":[]}`))
	chat := ConvertResponsesToOpenAIChatRequest("m", response, false)
	for _, body := range [][]byte{response, chat} {
		for _, key := range []string{"prompt_cache_key", "prompt_cache_options", "prompt_cache_retention"} {
			if gjson.GetBytes(body, key).Exists() {
				t.Fatalf("invented %s in %s", key, body)
			}
		}
	}
}
