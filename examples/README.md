**`llama-server` de `llama.cpp`** el servidor expone una API compatible con OpenAI.

Lo habitual es iniciar el servidor así:

```bash
llama-server \
  -m /models/Llama-3.1-8B-Instruct.gguf \
  --host 0.0.0.0 \
  --port 8088
```

Luego, desde Go:

```go
package main

import (
	"context"
	"fmt"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

func main() {
	client := openai.NewClient(
		option.WithBaseURL("http://localhost:8088/v1"),
		option.WithAPIKey("not-needed"), // llama-server normalmente no valida la API key
	)

	resp, err := client.Chat.Completions.New(context.Background(), openai.ChatCompletionNewParams{
		Model: "Llama-3.1-8B-Instruct", // el nombre puede ser cualquiera; llama.cpp suele ignorarlo
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage("Eres un asistente útil."),
			openai.UserMessage("¿Qué es Go?"),
		},
	})

	if err != nil {
		panic(err)
	}

	fmt.Println(resp.Choices[0].Message.Content)
}
```

## Streaming

También funciona el streaming:

```go
stream := client.Chat.Completions.NewStreaming(ctx, params)

for stream.Next() {
    chunk := stream.Current()
    fmt.Print(chunk.Choices[0].Delta.Content)
}

if err := stream.Err(); err != nil {
    panic(err)
}
```

## Cosas a tener en cuenta

Hay algunas diferencias respecto al servicio de OpenAI:

* `BaseURL` debe terminar en `/v1`.
* La API Key normalmente no se usa (a menos que hayas configurado autenticación).
* El campo `model` suele ignorarse porque el servidor ya tiene cargado un único modelo.
* No todos los endpoints de OpenAI están implementados.

## API Responses

Si tu versión reciente de `llama-server` implementa `/v1/responses`, también podrías usar:

```go
resp, err := client.Responses.New(ctx, openai.ResponseNewParams{
	Model: openai.String("ignored"),
	Input: openai.String("Hola"),
})
```

Pero **no todas las versiones de `llama.cpp` soportan todavía `Responses`**. La API `Chat Completions` sigue siendo la opción más compatible.

## Arquitectura recomendada

Una buena práctica es encapsular el cliente detrás de una interfaz, por ejemplo:

```go
type LLM interface {
    Chat(ctx context.Context, messages []Message) (string, error)
}
```

Y tener implementaciones como:

* `OpenAIProvider`
* `LlamaCPPProvider`
* `OllamaProvider`

En el caso de `LlamaCPPProvider`, solo cambias la configuración del cliente:

```go
client := openai.NewClient(
    option.WithBaseURL(cfg.BaseURL),
    option.WithAPIKey(cfg.APIKey),
)
```

De ese modo, el resto de tu aplicación no necesita saber si el modelo se ejecuta localmente o en OpenAI. Esta aproximación facilita cambiar de proveedor o soportar varios backends con un mínimo de cambios.
