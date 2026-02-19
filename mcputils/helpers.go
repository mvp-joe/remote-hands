package mcputils

import (
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
)

// JSONToolResult converts a response struct to an MCP tool result
func JSONToolResult(data interface{}) (*mcp.CallToolResult, error) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal response: %w", err)
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.NewTextContent(string(jsonData)),
		},
	}, nil
}

// ErrorToolResult creates an error tool result
func ErrorToolResult(errorType string, err error) (*mcp.CallToolResult, error) {
	errorResp := map[string]interface{}{
		"success": false,
		"error": map[string]interface{}{
			"error_type":       errorType,
			"attempted_action": "parse_arguments",
			"details": map[string]interface{}{
				"error": err.Error(),
			},
		},
	}

	return JSONToolResult(errorResp)
}
