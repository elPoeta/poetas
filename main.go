package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/elPoeta/poetas/internal/api"
	"github.com/elPoeta/poetas/internal/provider"
	"github.com/elPoeta/poetas/internal/ui"
)

const systemPrompt = `You are a coding assistant named Poetas running in a terminal.

Tool guidance:

For acting on the filesystem (creating, editing, running things), use bash, read_file, and write_file directly. Writes are mediated by a diff approval modal, so propose changes naturally — the user reviews each one.

For READ-ONLY INVESTIGATION you SHOULD call delegate_research rather than reading files yourself. This includes questions like:
- "where is X defined?"
- "what fields does Y have?"
- "look at the structure of Z"
- "find references to A in the code"
- "summarize how B works"

The subagent has its own context window, so it can do many reads without cluttering yours. Prefer delegating even when you think one or two reads would do it. Only skip the subagent if the question is about a single file the user has already shown you. After delegating, present the subagent's findings directly.

Be concise. Be honest when you don't know — guessing is worse than saying "I'd need to read X to answer that." Match the user's language: if they write in Spanish, answer in Spanish.`

var activeSysPrompt string

func envTruthy(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

var scanner = bufio.NewScanner(os.Stdin)

var tools = []api.ToolDef{
	{

		Name:        "bash",
		Description: "Run a shell command and return its combined stdout/stderr.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{"type": "string", "description": "The command to run."},
			},
			"required": []string{"command"},
		},
	},

	{
		Name:        "read_file",
		Description: "Read the contents of a file at the given path.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string", "description": "Filesystem path to read."},
			},
			"required": []string{"path"},
		},
	},
	{

		Name:        "write_file",
		Description: "Write content to a file (creating or overwriting it).",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":    map[string]any{"type": "string", "description": "Filesystem path to write."},
				"content": map[string]any{"type": "string", "description": "The bytes to write."},
			},
			"required": []string{"path", "content"},
		},
	},
}

func main() {
	ctx := context.Background()

	activeSysPrompt = systemPrompt + loadAgentsContext()

	llm := newProvider(activeSysPrompt)
	messages := []api.Message{}

	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	ui.PrintBanner()

	for {
		fmt.Print("> ")

		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				fmt.Fprintln(os.Stderr, err)
			}
			return
		}

		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}
		if input == "exit" {
			return
		}

		// Agregar mensaje del usuario al historial.
		messages = append(messages, api.Message{
			Role: api.RoleUser,
			Content: []api.Block{
				{
					Type: api.BlockText,
					Text: input,
				},
			},
		})

		// Bucle del agente: continúa hasta que el modelo ya no solicite tools.
		for {
			sp := ui.StartSpinner("thinking...")
			res, err := llm.Send(ctx, messages, tools)
			sp.Stop()

			if err != nil {
				fmt.Fprintf(os.Stderr, "send: %v\n", err)
				break
			}

			// Guardar respuesta del asistente.
			messages = append(messages, api.Message{
				Role:    api.RoleAssistant,
				Content: res.Content,
			})

			// Mostrar texto al usuario.
			for _, b := range res.Content {
				if b.Type == api.BlockText {
					fmt.Println(b.Text)
				}
			}

			// Si terminó normalmente, volver a pedir input.
			if res.StopReason != api.StopToolUse {
				break
			}

			// Ejecutar todas las tools solicitadas.
			for _, b := range res.Content {
				if b.Type != api.BlockToolUse {
					continue
				}

				output, isError := executeTool(b.ToolName, b.ToolInput)

				messages = append(messages, api.Message{
					Role: api.RoleUser,
					Content: []api.Block{
						{
							Type:       api.BlockToolResult,
							ToolUseID:  b.ToolUseID,
							ToolResult: output,
							IsError:    isError,
						},
					},
				})
			}

			// El for interno vuelve a llamar a llm.Send()
			// con los tool_results recién agregados.
		}
	}
}

// KnownProviders is the ordered list of names accepted by /provider and by
// $LLM_PROVIDER. Update both this list and newProviderByName when adding a
// backend.
var KnownProviders = []string{"openai", "llama"}

// newProvider picks the initial provider at startup, honoring $LLM_PROVIDER
// and $LLM_MODEL as optional defaults. Mid-session swaps go through the
// /provider slash command instead.
func newProvider(sysPrompt string) provider.Provider {
	name := os.Getenv("LLM_PROVIDER")
	if name == "" {
		name = "llama"
	}
	p, err := newProviderByName(name, os.Getenv("LLM_MODEL"), sysPrompt)
	if err != nil {
		fmt.Fprintf(os.Stderr, "provider: %v (falling back to local)\n", err)
		p, _ = newProviderByName("llama", "", sysPrompt)
	}
	return p
}

// newProviderByName constructs a provider by short name. An empty model
// string falls back to that provider's default. Unknown names error.
func newProviderByName(name, model, sysPrompt string) (provider.Provider, error) {
	switch name {
	case "openai":
		if model == "" {
			model = "gpt-5-codex"
		}
		return provider.NewOpenAIProvider(model, sysPrompt, 8192, ""), nil
	case "llama":
		if model == "" {
			model = "local-model"
		}
		return provider.NewOpenAIProvider(model, sysPrompt, 8192, "http://localhost:8088/v1"), nil
	case "mock":
		return provider.NewMockProvider(), nil

	default:
		return nil, fmt.Errorf("unknown provider %q (try one of %s)", name, strings.Join(KnownProviders, ", "))
	}
}

func loadAgentsContext() string {
	data, err := os.ReadFile("AGENTS.md")
	if err != nil {
		return ""
	}
	return "\n\n# Project context (from AGENTS.md)\n\n" + string(data)
}

func executeTool(name, rawInput string) (string, bool) {
	fmt.Printf("[tool] %s %s\n", name, rawInput)
	if !confirm("approve?") {
		return "user denied this tool call", true
	}
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

func confirm(prompt string) bool {
	fmt.Printf("%s [y/n] ", prompt)
	if !scanner.Scan() {
		return false
	}
	a := strings.ToLower(strings.TrimSpace(scanner.Text()))
	return a == "y" || a == "yes"
}
