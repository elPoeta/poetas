package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

const systemPrompt = `You are a coding assistant named Poetas running in a terminal. You have three tools:
bash, read_file, write_file. Be concise.`

var client = openai.NewClient(
	option.WithBaseURL("http://localhost:8088/v1"),
	option.WithAPIKey("not-needed"), // llama-server normalmente no valida la API key
)

var tools = []openai.ChatCompletionToolParam{
	{
		Function: openai.FunctionDefinitionParam{
			Name:        "bash",
			Description: openai.String("Run a shell command and return its combined stdout/stderr."),
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"command": map[string]any{"type": "string", "description": "The command to run."},
				},
				"required": []string{"command"},
			},
		},
	},
	{
		Function: openai.FunctionDefinitionParam{
			Name:        "read_file",
			Description: openai.String("Read the contents of a file at the given path."),
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{"type": "string", "description": "Filesystem path to read."},
				},
				"required": []string{"path"},
			},
		},
	},
	{
		Function: openai.FunctionDefinitionParam{
			Name:        "write_file",
			Description: openai.String("Write content to a file (creating or overwriting it)."),
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":    map[string]any{"type": "string", "description": "Filesystem path to write."},
					"content": map[string]any{"type": "string", "description": "The bytes to write."},
				},
				"required": []string{"path", "content"},
			},
		},
	},
}

func main() {

	ctx := context.Background()
	var messages []openai.ChatCompletionMessageParamUnion = []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(systemPrompt),
	}
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	for {
		fmt.Print("> ")
		if err := scanner.Err(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return
		}
		if !scanner.Scan() {
			return
		}

		input := strings.TrimSpace(scanner.Text())
		if input == "exit" {
			break
		}
		if input == "" {
			continue
		}

		messages = append(messages,
			openai.UserMessage(input),
		)

		messages = agentLoop(ctx, messages)
	}

}

func agentLoop(
	ctx context.Context,
	messages []openai.ChatCompletionMessageParamUnion,
) []openai.ChatCompletionMessageParamUnion {

	for {
		resp, err := client.Chat.Completions.New(ctx,
			openai.ChatCompletionNewParams{
				Model:    "llama",
				Messages: messages,
				Tools:    tools,
			},
		)
		if err != nil {
			fmt.Println(err)
			return messages
		}

		choice := resp.Choices[0]

		// agregar el mensaje del assistant
		messages = append(messages, choice.Message.ToParam())

		// ¿el modelo terminó?
		if len(choice.Message.ToolCalls) == 0 {
			fmt.Println(choice.Message.Content)
			return messages
		}

		// ejecutar cada tool
		for _, tc := range choice.Message.ToolCalls {

			result, isErr := executeTool(
				tc.Function.Name,
				string(tc.Function.Arguments),
			)

			if isErr {
				result = "ERROR: " + result
			}

			messages = append(messages,
				openai.ToolMessage(
					result,
					tc.ID,
				),
			)
		}
	}
}

func executeTool(name, rawInput string) (string, bool) {
	fmt.Printf("[tool] %s %s\n", name, rawInput)
	switch name {
	case "bash":
		var in struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal([]byte(rawInput), &in); err != nil {
			return err.Error(), true
		}
		out, err := exec.Command("sh", "-c", in.Command).CombinedOutput()
		if err != nil {
			return fmt.Sprintf("%s\n[exit error: %v]", out, err), true
		}
		return string(out), false

	case "read_file":
		var in struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal([]byte(rawInput), &in); err != nil {
			return err.Error(), true
		}
		data, err := os.ReadFile(in.Path)
		if err != nil {
			return err.Error(), true
		}
		return string(data), false

	case "write_file":
		var in struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal([]byte(rawInput), &in); err != nil {
			return err.Error(), true
		}
		if err := os.WriteFile(in.Path, []byte(in.Content), 0644); err != nil {
			return err.Error(), true
		}
		return "wrote " + in.Path, false

	default:
		return fmt.Sprintf("unknown tool: %s", name), true
	}
}
