package converters

import (
	"fmt"
	"github.com/soulteary/yunqi/internal/types"
)

// normalizeGeminiToolIDs assigns missing correlation IDs before converting a
// native request. Gemini may omit IDs, including parallel calls of one name.
// Match each result to the oldest unmatched call of that name, without mutating
// caller-owned request history.
func normalizeGeminiToolIDs(request *types.GeminiRequest) (*types.GeminiRequest, error) {
	if request == nil {
		return nil, fmt.Errorf("Gemini request is nil")
	}
	copyRequest := *request
	copyRequest.Contents = append([]types.GeminiContent(nil), request.Contents...)
	used := map[string]bool{}
	for _, content := range request.Contents {
		for _, part := range content.Parts {
			if part.FunctionCall != nil && part.FunctionCall.ID != "" {
				used[part.FunctionCall.ID] = true
			}
			if part.FunctionResponse != nil && part.FunctionResponse.ID != "" {
				used[part.FunctionResponse.ID] = true
			}
		}
	}
	pending := map[string][]string{}
	nextID := 0
	for ci, content := range request.Contents {
		copyRequest.Contents[ci].Parts = append([]types.GeminiPart(nil), content.Parts...)
		for pi, part := range content.Parts {
			if part.FunctionCall != nil {
				call := *part.FunctionCall
				if call.ID == "" {
					for {
						nextID++
						call.ID = fmt.Sprintf("gemini_call_%d", nextID)
						if !used[call.ID] {
							used[call.ID] = true
							break
						}
					}
				}
				pending[call.Name] = append(pending[call.Name], call.ID)
				copyRequest.Contents[ci].Parts[pi].FunctionCall = &call
			}
			if part.FunctionResponse != nil {
				response := *part.FunctionResponse
				queue := pending[response.Name]
				if response.ID == "" {
					if len(queue) == 0 {
						return nil, fmt.Errorf("Gemini tool result has no matching call")
					}
					response.ID = queue[0]
					pending[response.Name] = queue[1:]
				} else {
					for index, id := range queue {
						if id == response.ID {
							pending[response.Name] = append(queue[:index], queue[index+1:]...)
							break
						}
					}
				}
				copyRequest.Contents[ci].Parts[pi].FunctionResponse = &response
			}
		}
	}
	return &copyRequest, nil
}
