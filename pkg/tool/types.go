package tool

import "context"

// Tool is the public interface for external tool implementations.
// External packages should implement this interface to create custom tools.
//
// Example:
//
//	type MyTool struct{}
//
//	func (t *MyTool) Name() string { return "my_tool" }
//	func (t *MyTool) Description() string { return "My custom tool" }
//	func (t *MyTool) Parameters() map[string]any {
//	    return map[string]any{
//	        "type": "object",
//	        "properties": map[string]any{
//	            "input": map[string]any{
//	                "type": "string",
//	                "description": "Input parameter",
//	            },
//	        },
//	        "required": []string{"input"},
//	    }
//	}
//	func (t *MyTool) Execute(ctx context.Context, args map[string]any) *Result {
//	    // Tool implementation
//	    return &Result{
//	        ForLLM:  "Result for LLM",
//	        ForUser: "Result for user",
//	        Success: true,
//	    }
//	}
type Tool interface {
	// Name returns the tool name (used by LLM to call the tool).
	Name() string

	// Description returns the tool description (shown to LLM).
	Description() string

	// Parameters returns the JSON Schema for tool parameters.
	// Example:
	//   map[string]any{
	//       "type": "object",
	//       "properties": map[string]any{
	//           "param1": map[string]any{
	//               "type": "string",
	//               "description": "Parameter description",
	//           },
	//       },
	//       "required": []string{"param1"},
	//   }
	Parameters() map[string]any

	// Execute executes the tool with the given arguments.
	// Returns a Result containing output for both LLM and user.
	Execute(ctx context.Context, args map[string]any) *Result
}

// Result is the return value from Tool.Execute.
type Result struct {
	// ForLLM is the result shown to the LLM (may be sanitized).
	ForLLM string

	// ForUser is the result shown to the end user.
	ForUser string

	// Success indicates whether the tool execution succeeded.
	Success bool

	// Metadata holds additional structured data from tool execution.
	// Useful for passing quantitative results, scores, or other programmatic data.
	Metadata map[string]any
}
