package provider

import (
	"context"
	"os"
	"strings"
	"sync"

	"github.com/elPoeta/poetas/internal/api"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/packages/param"
	"github.com/openai/openai-go/shared"
)

type OpenAIProvider struct {
	client    openai.Client
	model     string
	system    string
	maxTokens int64

	mu sync.Mutex
}

func NewOpenAIProvider(model, system string, maxTokens int64, baseURL string) *OpenAIProvider {
	opts := []option.RequestOption{}
	if baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
		// Los servidores locales no validan la API key, pero el SDK se
		// niega a construirse sin una. Un placeholder es suficiente.
		if os.Getenv("OPENAI_API_KEY") == "" {
			opts = append(opts, option.WithAPIKey("local"))
		}
	}
	//opts = append(opts, option.WithDebugLog(nil))
	return &OpenAIProvider{client: openai.NewClient(opts...), model: model, system: system, maxTokens: maxTokens}
}

func (p *OpenAIProvider) Model() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.model
}

func (p *OpenAIProvider) SetModel(name string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.model = name
}

func (p *OpenAIProvider) Send(ctx context.Context, messages []api.Message, tools []api.ToolDef) (api.Response, error) {
	p.mu.Lock()
	model := p.model
	p.mu.Unlock()

	//reqSummary := fmt.Sprintf("→ openai.Send model=%s msgs=%d tools=%d", model, len(messages), len(tools))
	//reqID := debug.Recordp("provider", debug.LevelInfo, reqSummary, marshalRequestPayload(model, p.system, p.maxTokens, messages, tools))
	//start := time.Now()
	resp, err := p.client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model:               shared.ChatModel(model),
		Messages:            p.toMessages(messages),
		Tools:               p.toTools(tools),
		MaxCompletionTokens: param.NewOpt(p.maxTokens),
	})

	//elapsed := time.Since(start).Round(time.Millisecond)
	if err != nil {
		//	debug.Recordfc(reqID, "provider", debug.LevelError, "← openai.Send error (%v): %v", elapsed, err)
		return api.Response{}, err
	}
	if len(resp.Choices) == 0 {
		return api.Response{StopReason: api.StopOther}, nil
	}
	choice := resp.Choices[0]

	out := api.Response{StopReason: fromFinishReason(choice.FinishReason)}
	if choice.Message.Content != "" {
		out.Content = append(out.Content, api.Block{Type: api.BlockText, Text: choice.Message.Content})
	}
	for _, tc := range choice.Message.ToolCalls {
		out.Content = append(out.Content, api.Block{
			Type:      api.BlockToolUse,
			ToolUseID: tc.ID,
			ToolName:  tc.Function.Name,
			ToolInput: tc.Function.Arguments,
		})
	}

	//out.Usage = api.Usage{
	//	InputTokens:     int(resp.Usage.PromptTokens),
	//		OutputTokens:    int(resp.Usage.CompletionTokens),
	//	CacheReadTokens: int(resp.Usage.PromptTokensDetails.CachedTokens),
	// OpenAI doesn't expose cache-creation separately; cached_tokens is
	// the only cache figure they return, and it maps to cache reads.
	//}
	//p.mu.Lock()
	//	p.total = p.total.Add(out.Usage)
	//	p.mu.Unlock()
	//	respSummary := fmt.Sprintf("← openai.Send (%v in=%d out=%d cache=%d stop=%s)",
	//		elapsed, out.Usage.InputTokens, out.Usage.OutputTokens, out.Usage.CacheReadTokens, out.StopReason)
	//	debug.Recordpc(reqID, "provider", debug.LevelInfo, respSummary, marshalResponsePayload(out, elapsed))
	return out, nil
}

func (p *OpenAIProvider) toMessages(messages []api.Message) []openai.ChatCompletionMessageParamUnion {
	out := make([]openai.ChatCompletionMessageParamUnion, 0, len(messages)+1)
	if p.system != "" {
		out = append(out, openai.SystemMessage(p.system))
	}
	for _, m := range messages {
		switch m.Role {
		case api.RoleUser:
			// Split this message: gather text into one UserMessage, emit each
			// tool_result as its own ToolMessage.
			var textParts []string
			var toolResults []openai.ChatCompletionMessageParamUnion
			for _, b := range m.Content {
				switch b.Type {
				case api.BlockText:
					textParts = append(textParts, b.Text)
				case api.BlockToolResult:
					toolResults = append(toolResults, openai.ToolMessage(b.ToolResult, b.ToolUseID))
				}
			}
			if len(textParts) > 0 {
				out = append(out, openai.UserMessage(strings.Join(textParts, "\n")))
			}
			out = append(out, toolResults...)

		case api.RoleAssistant:
			var text strings.Builder
			var toolCalls []openai.ChatCompletionMessageToolCallParam
			for _, b := range m.Content {
				switch b.Type {
				case api.BlockText:
					if text.Len() > 0 {
						text.WriteString("\n")
					}
					text.WriteString(b.Text)
				case api.BlockToolUse:
					toolCalls = append(toolCalls, openai.ChatCompletionMessageToolCallParam{
						ID: b.ToolUseID,
						Function: openai.ChatCompletionMessageToolCallFunctionParam{
							Name:      b.ToolName,
							Arguments: b.ToolInput,
						},
					})
				}
			}
			msg := openai.ChatCompletionAssistantMessageParam{}
			if text.Len() > 0 {
				msg.Content.OfString = param.NewOpt(text.String())
			}
			if len(toolCalls) > 0 {
				msg.ToolCalls = toolCalls
			}
			out = append(out, openai.ChatCompletionMessageParamUnion{OfAssistant: &msg})
		}
	}
	return out
}

func (p *OpenAIProvider) toTools(tools []api.ToolDef) []openai.ChatCompletionToolParam {
	if len(tools) == 0 {
		return nil
	}
	out := make([]openai.ChatCompletionToolParam, 0, len(tools))
	for _, t := range tools {
		// parameters := map[string]any{
		// 	"type":       "object",
		// 	"properties": t.InputSchema,
		// }
		// if len(t.Required) > 0 {
		// 	parameters["required"] = t.Required
		// }
		// out = append(out, openai.ChatCompletionToolParam{
		// 	Function: shared.FunctionDefinitionParam{
		// 		Name:        t.Name,
		// 		Description: param.NewOpt(t.Description),
		// 		Parameters:  shared.FunctionParameters(parameters),
		// 	},
		// })
		out = append(out, openai.ChatCompletionToolParam{
			Function: shared.FunctionDefinitionParam{
				Name:        t.Name,
				Description: param.NewOpt(t.Description),
				Parameters:  shared.FunctionParameters(t.InputSchema),
			},
		})
	}
	return out
}

func fromFinishReason(r string) api.StopReason {
	switch r {
	case "stop":
		return api.StopEndTurn
	case "tool_calls":
		return api.StopToolUse
	default:
		return api.StopOther
	}
}
