package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// ApplyGroupCodexInstructions applies a group's instructions once at the
// protocol boundary. Callers should invoke it before account failover loops.
func ApplyGroupCodexInstructions(body []byte, endpoint string, group *Group) ([]byte, bool, error) {
	if group == nil || !group.CodexInstructionsEnabled {
		return body, false, nil
	}
	instructions := strings.TrimSpace(group.CodexInstructions)
	if instructions == "" {
		return body, false, nil
	}

	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var request map[string]any
	if err := decoder.Decode(&request); err != nil {
		return body, false, fmt.Errorf("decode request for group Codex instructions: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return body, false, fmt.Errorf("decode request for group Codex instructions: trailing JSON content")
	}
	if request == nil {
		return body, false, fmt.Errorf("request body must be a JSON object")
	}

	var changed bool
	switch strings.ToLower(strings.TrimSpace(endpoint)) {
	case "responses":
		existing, ok := request["instructions"].(string)
		if request["instructions"] != nil && !ok {
			return body, false, fmt.Errorf("instructions must be a string")
		}
		merged := mergeGroupInstruction(instructions, existing)
		if existing != merged {
			request["instructions"] = merged
			changed = true
		}
	case "chat_completions":
		messages, ok := request["messages"].([]any)
		if !ok {
			// Responses-shaped compatibility requests still use top-level instructions.
			if existing, isString := request["instructions"].(string); isString || request["instructions"] == nil {
				merged := mergeGroupInstruction(instructions, existing)
				if existing != merged {
					request["instructions"] = merged
					changed = true
				}
			}
			break
		}
		request["messages"], changed = prependSystemMessage(messages, instructions)
	case "messages":
		changed = applyAnthropicSystemInstruction(request, instructions)
	default:
		return body, false, nil
	}
	if !changed {
		return body, false, nil
	}
	out, err := json.Marshal(request)
	if err != nil {
		return body, false, fmt.Errorf("encode request with group Codex instructions: %w", err)
	}
	return out, true, nil
}

func mergeGroupInstruction(groupInstruction, existing string) string {
	groupInstruction = strings.TrimSpace(groupInstruction)
	existing = strings.TrimSpace(existing)
	if existing == "" {
		return groupInstruction
	}
	if existing == groupInstruction || strings.HasPrefix(existing, groupInstruction+"\n\n") {
		return existing
	}
	return groupInstruction + "\n\n" + existing
}

func prependSystemMessage(messages []any, instructions string) ([]any, bool) {
	if len(messages) > 0 {
		if first, ok := messages[0].(map[string]any); ok && strings.EqualFold(fmt.Sprint(first["role"]), "system") {
			if content, ok := first["content"].(string); ok {
				merged := mergeGroupInstruction(instructions, content)
				if merged == content {
					return messages, false
				}
				first["content"] = merged
				return messages, true
			}
		}
	}
	return append([]any{map[string]any{"role": "system", "content": instructions}}, messages...), true
}

func applyAnthropicSystemInstruction(request map[string]any, instructions string) bool {
	system, exists := request["system"]
	if !exists || system == nil {
		request["system"] = instructions
		return true
	}
	if existing, ok := system.(string); ok {
		merged := mergeGroupInstruction(instructions, existing)
		if merged == existing {
			return false
		}
		request["system"] = merged
		return true
	}
	if blocks, ok := system.([]any); ok {
		if len(blocks) > 0 {
			if first, ok := blocks[0].(map[string]any); ok && first["type"] == "text" {
				if text, ok := first["text"].(string); ok && strings.TrimSpace(text) == instructions {
					return false
				}
			}
		}
		request["system"] = append([]any{map[string]any{"type": "text", "text": instructions}}, blocks...)
		return true
	}
	// Preserve malformed client input semantics as much as possible and let the
	// existing protocol validator report the original shape later.
	request["system"] = instructions
	return true
}
