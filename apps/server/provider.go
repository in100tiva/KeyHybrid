package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
)

const (
	ProviderOpenAI     = "openai"
	ProviderAnthropic  = "anthropic"
	ProviderGemini     = "gemini"
	ProviderGroq       = "groq"
	ProviderOpenRouter = "openrouter"
)

// Normaliza o nome do provedor
func normalizeProvider(p string) string {
	p = strings.ToLower(strings.TrimSpace(p))
	switch p {
	case "google", "gemini":
		return ProviderGemini
	case "anthropic", "claude":
		return ProviderAnthropic
	case "groq":
		return ProviderGroq
	case "openai":
		return ProviderOpenAI
	case "openrouter":
		return ProviderOpenRouter
	default:
		return p
	}
}

// Modelos conhecidos por provedor
var defaultProviderModels = map[string][]string{
	ProviderAnthropic: {
		"claude-3-7-sonnet",
		"claude-3-5-sonnet",
		"claude-3-5-sonnet-20241022",
		"claude-3-5-haiku",
		"claude-3-opus",
		"claude-haiku-5-5",
	},
	ProviderGemini: {
		"gemini-2.5-flash",
		"gemini-2.5-pro",
		"gemini-2.0-flash",
		"gemini-1.5-pro",
		"gemini-1.5-flash",
	},
	ProviderGroq: {
		"llama-3.3-70b-versatile",
		"llama-3.1-8b-instant",
		"mixtral-8x7b-32768",
		"gemma2-9b-it",
	},
	ProviderOpenAI: {
		"gpt-4o",
		"gpt-4o-mini",
		"o1",
		"o3-mini",
		"gpt-4-turbo",
	},
	ProviderOpenRouter: {
		"openrouter/auto",
	},
}

// Extrai prefixo do modelo (ex: "anthropic/claude-3-5-sonnet" -> ("anthropic", "claude-3-5-sonnet"))
func parseModelPrefix(model string) (string, string) {
	model = strings.TrimSpace(model)
	slashIdx := strings.Index(model, "/")
	if slashIdx <= 0 {
		return "", model
	}
	prefix := model[:slashIdx]
	clean := model[slashIdx+1:]
	normalized := normalizeProvider(prefix)
	switch normalized {
	case ProviderAnthropic, ProviderGemini, ProviderGroq, ProviderOpenAI, ProviderOpenRouter:
		return normalized, clean
	default:
		return "", model
	}
}

// Resolução de provedor com precedência:
// 1. Header X-Provider (se fornecido)
// 2. Prefixo no model id (ex: anthropic/claude-3-5-sonnet)
// 3. requestedAlias (kh-<alias> ou alias no payload)
// 4. Provedor ativo (active=true)
// 5. Primeiro provedor de IA cadastrado
func resolveAIProvider(
	requestedAlias string,
	headerProvider string,
	requestedModel string,
	records []*core.Record,
) (matchedRecord *core.Record, cleanModel string, provider string) {
	cleanModel = requestedModel
	headerProvider = normalizeProvider(headerProvider)
	prefixProvider, strippedModel := parseModelPrefix(requestedModel)

	var targetProvider string
	if headerProvider != "" {
		targetProvider = headerProvider
		if prefixProvider != "" {
			cleanModel = strippedModel
		}
	} else if prefixProvider != "" {
		targetProvider = prefixProvider
		cleanModel = strippedModel
	}

	// 1. Tentar encontrar por targetProvider se identificado por header ou prefixo
	if targetProvider != "" {
		for _, r := range records {
			p := normalizeProvider(r.GetString("provider"))
			a := normalizeProvider(r.GetString("alias"))
			if p == targetProvider || a == targetProvider {
				return r, cleanModel, p
			}
		}
	}

	// 2. Tentar encontrar por requestedAlias
	if requestedAlias != "" && !strings.EqualFold(requestedAlias, "default") {
		for _, r := range records {
			if strings.EqualFold(r.GetString("alias"), requestedAlias) {
				p := normalizeProvider(r.GetString("provider"))
				if targetProvider == "" && prefixProvider != "" {
					cleanModel = strippedModel
				}
				return r, cleanModel, p
			}
		}
	}

	// 3. Fallback: chave ativa
	for _, r := range records {
		if r.GetBool("active") {
			p := normalizeProvider(r.GetString("provider"))
			if prefixProvider == p {
				cleanModel = strippedModel
			}
			return r, cleanModel, p
		}
	}

	// 4. Fallback final: primeira chave
	if len(records) > 0 {
		r := records[0]
		p := normalizeProvider(r.GetString("provider"))
		if prefixProvider == p {
			cleanModel = strippedModel
		}
		return r, cleanModel, p
	}

	return nil, cleanModel, ""
}

// Retorna a URL de destino da API de chat para o provedor
func resolveProviderChatURL(provider string, customBaseURL string) string {
	if customBaseURL != "" {
		trimmed := strings.TrimRight(customBaseURL, "/")
		if !strings.HasSuffix(trimmed, "/chat/completions") && !strings.HasSuffix(trimmed, "/messages") {
			if normalizeProvider(provider) == ProviderAnthropic {
				return trimmed + "/v1/messages"
			}
			return trimmed + "/chat/completions"
		}
		return customBaseURL
	}

	switch normalizeProvider(provider) {
	case ProviderAnthropic:
		return "https://api.anthropic.com/v1/messages"
	case ProviderGemini:
		return "https://generativelanguage.googleapis.com/v1beta/openai/chat/completions"
	case ProviderGroq:
		return "https://api.groq.com/openai/v1/chat/completions"
	case ProviderOpenAI:
		return "https://api.openai.com/v1/chat/completions"
	default:
		return "https://openrouter.ai/api/v1/chat/completions"
	}
}

// --- Estruturas e Tradução Anthropic ---

type AnthropicMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type AnthropicRequest struct {
	Model         string             `json:"model"`
	Messages      []AnthropicMessage `json:"messages"`
	System        string             `json:"system,omitempty"`
	MaxTokens     int                `json:"max_tokens"`
	Temperature   *float64           `json:"temperature,omitempty"`
	TopP          *float64           `json:"top_p,omitempty"`
	StopSequences []string           `json:"stop_sequences,omitempty"`
	Stream        bool               `json:"stream,omitempty"`
}

type AnthropicContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type AnthropicResponse struct {
	ID         string                  `json:"id"`
	Type       string                  `json:"type"`
	Role       string                  `json:"role"`
	Model      string                  `json:"model"`
	Content    []AnthropicContentBlock `json:"content"`
	StopReason string                  `json:"stop_reason"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// Traduz payload OpenAI -> Anthropic
func translateOpenAIToAnthropic(payload map[string]any, cleanModel string) (*AnthropicRequest, error) {
	req := &AnthropicRequest{
		Model:     cleanModel,
		MaxTokens: 4096, // default exigido pela Anthropic
	}

	// Modelo
	if req.Model == "" {
		req.Model = "claude-3-5-sonnet-20241022"
	}
	req.Model = strings.TrimPrefix(req.Model, "anthropic/")

	// Max tokens
	if maxT, ok := payload["max_tokens"].(float64); ok && maxT > 0 {
		req.MaxTokens = int(maxT)
	} else if maxComp, ok := payload["max_completion_tokens"].(float64); ok && maxComp > 0 {
		req.MaxTokens = int(maxComp)
	}

	// Temperature
	if temp, ok := payload["temperature"].(float64); ok {
		req.Temperature = &temp
	}

	// Top_p
	if topP, ok := payload["top_p"].(float64); ok {
		req.TopP = &topP
	}

	// Stop sequences
	if stop, ok := payload["stop"]; ok {
		switch v := stop.(type) {
		case string:
			if v != "" {
				req.StopSequences = []string{v}
			}
		case []any:
			var stops []string
			for _, item := range v {
				if s, ok := item.(string); ok && s != "" {
					stops = append(stops, s)
				}
			}
			if len(stops) > 0 {
				req.StopSequences = stops
			}
		}
	}

	// Stream
	if stream, ok := payload["stream"].(bool); ok {
		req.Stream = stream
	}

	// Extrair system e messages
	rawMsgs, _ := payload["messages"].([]any)
	var systemParts []string
	var anthropicMsgs []AnthropicMessage

	for _, raw := range rawMsgs {
		msgMap, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		role, _ := msgMap["role"].(string)
		content := msgMap["content"]

		if role == "system" {
			switch c := content.(type) {
			case string:
				if c != "" {
					systemParts = append(systemParts, c)
				}
			case []any:
				for _, part := range c {
					if pMap, ok := part.(map[string]any); ok {
						if text, ok := pMap["text"].(string); ok && text != "" {
							systemParts = append(systemParts, text)
						}
					}
				}
			}
			continue
		}

		anthropicRole := "user"
		if role == "assistant" {
			anthropicRole = "assistant"
		}

		// Anthropic exige alternância estrita entre user e assistant
		if len(anthropicMsgs) > 0 && anthropicMsgs[len(anthropicMsgs)-1].Role == anthropicRole {
			prev := anthropicMsgs[len(anthropicMsgs)-1]
			prevStr, prevIsStr := prev.Content.(string)
			currStr, currIsStr := content.(string)
			if prevIsStr && currIsStr {
				anthropicMsgs[len(anthropicMsgs)-1].Content = prevStr + "\n\n" + currStr
				continue
			}
		}

		anthropicMsgs = append(anthropicMsgs, AnthropicMessage{
			Role:    anthropicRole,
			Content: content,
		})
	}

	if len(systemParts) > 0 {
		req.System = strings.Join(systemParts, "\n\n")
	}

	// Se não houver mensagens ou a primeira for assistant, garantir que comece com user
	if len(anthropicMsgs) == 0 {
		anthropicMsgs = append(anthropicMsgs, AnthropicMessage{
			Role:    "user",
			Content: "Olá",
		})
	} else if anthropicMsgs[0].Role != "user" {
		anthropicMsgs = append([]AnthropicMessage{{
			Role:    "user",
			Content: "...",
		}}, anthropicMsgs...)
	}

	req.Messages = anthropicMsgs
	return req, nil
}

// Traduz resposta não-streaming Anthropic -> formato OpenAI
func translateAnthropicToOpenAI(resp *AnthropicResponse, originalModel string) map[string]any {
	var textParts []string
	for _, block := range resp.Content {
		if block.Type == "text" && block.Text != "" {
			textParts = append(textParts, block.Text)
		}
	}
	content := strings.Join(textParts, "")

	finishReason := "stop"
	switch resp.StopReason {
	case "max_tokens":
		finishReason = "length"
	case "end_turn", "stop_sequence":
		finishReason = "stop"
	case "tool_use":
		finishReason = "tool_calls"
	}

	modelOut := originalModel
	if !strings.HasPrefix(modelOut, "anthropic/") {
		modelOut = "anthropic/" + resp.Model
	}

	return map[string]any{
		"id":      "chatcmpl-" + resp.ID,
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   modelOut,
		"choices": []map[string]any{
			{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": content,
				},
				"finish_reason": finishReason,
			},
		},
		"usage": map[string]any{
			"prompt_tokens":     resp.Usage.InputTokens,
			"completion_tokens": resp.Usage.OutputTokens,
			"total_tokens":      resp.Usage.InputTokens + resp.Usage.OutputTokens,
		},
	}
}

// Traduz streaming Anthropic SSE em tempo real -> OpenAI SSE
func pipeAnthropicStreamToOpenAI(ctx context.Context, w http.ResponseWriter, upstreamBody io.Reader, originalModel string) error {
	flusher, isFlusher := w.(http.Flusher)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	if isFlusher {
		flusher.Flush()
	}

	reader := bufio.NewReader(upstreamBody)
	messageID := fmt.Sprintf("msg_%d", time.Now().UnixNano())
	modelOut := originalModel
	if !strings.HasPrefix(modelOut, "anthropic/") {
		modelOut = "anthropic/" + modelOut
	}
	created := time.Now().Unix()

	sentRole := false

	sendChunk := func(delta map[string]any, finishReason any) {
		chunk := map[string]any{
			"id":      "chatcmpl-" + messageID,
			"object":  "chat.completion.chunk",
			"created": created,
			"model":   modelOut,
			"choices": []map[string]any{
				{
					"index":         0,
					"delta":         delta,
					"finish_reason": finishReason,
				},
			},
		}
		data, _ := json.Marshal(chunk)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
		if isFlusher {
			flusher.Flush()
		}
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}

		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}

		dataJSON := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if dataJSON == "" || dataJSON == "[DONE]" {
			continue
		}

		var rawEvt map[string]any
		if err := json.Unmarshal([]byte(dataJSON), &rawEvt); err != nil {
			continue
		}

		evtType, _ := rawEvt["type"].(string)
		switch evtType {
		case "message_start":
			if msg, ok := rawEvt["message"].(map[string]any); ok {
				if id, ok := msg["id"].(string); ok && id != "" {
					messageID = id
				}
				if m, ok := msg["model"].(string); ok && m != "" {
					modelOut = "anthropic/" + m
				}
			}
			if !sentRole {
				sendChunk(map[string]any{"role": "assistant", "content": ""}, nil)
				sentRole = true
			}

		case "content_block_delta":
			if delta, ok := rawEvt["delta"].(map[string]any); ok {
				if text, ok := delta["text"].(string); ok && text != "" {
					if !sentRole {
						sendChunk(map[string]any{"role": "assistant"}, nil)
						sentRole = true
					}
					sendChunk(map[string]any{"content": text}, nil)
				}
			}

		case "message_delta":
			finish := "stop"
			if delta, ok := rawEvt["delta"].(map[string]any); ok {
				if stopReason, ok := delta["stop_reason"].(string); ok {
					switch stopReason {
					case "max_tokens":
						finish = "length"
					case "end_turn", "stop_sequence":
						finish = "stop"
					case "tool_use":
						finish = "tool_calls"
					}
				}
			}
			sendChunk(map[string]any{}, finish)

		case "message_stop":
			_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
			if isFlusher {
				flusher.Flush()
			}
			return nil
		}
	}

	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	if isFlusher {
		flusher.Flush()
	}
	return nil
}

// --- Agregação de Modelos para GET /v1/models ---

type ModelData struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

type ModelListResponse struct {
	Object string      `json:"object"`
	Data   []ModelData `json:"data"`
}

// Agrega modelos de todos os provedores de IA cadastrados no cofre
func aggregateEnabledModels(ctx context.Context, records []*core.Record) ModelListResponse {
	seen := make(map[string]bool)
	var models []ModelData
	now := time.Now().Unix()

	addModel := func(id, ownedBy string) {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		models = append(models, ModelData{
			ID:      id,
			Object:  "model",
			Created: now,
			OwnedBy: ownedBy,
		})
	}

	for _, r := range records {
		provider := normalizeProvider(r.GetString("provider"))
		if provider == "" {
			provider = ProviderOpenRouter
		}
		isActive := r.GetBool("active")

		// Modelos pré-definidos do provedor
		if presetList, ok := defaultProviderModels[provider]; ok {
			for _, m := range presetList {
				prefixed := m
				if !strings.Contains(m, "/") {
					prefixed = provider + "/" + m
				}
				addModel(prefixed, provider)
				if isActive && !strings.Contains(m, "/") {
					addModel(m, provider)
				}
			}
		}

		// Se for OpenRouter e tiver chave, tentar buscar modelos dinamicamente com timeout curto
		if provider == ProviderOpenRouter {
			secretKey := decryptSecretAtRest(r.GetString("secret_key"))
			if secretKey != "" {
				client := &http.Client{Timeout: 2 * time.Second}
				req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://openrouter.ai/api/v1/models/user", nil)
				if err == nil {
					req.Header.Set("Authorization", "Bearer "+secretKey)
					if resp, err := client.Do(req); err == nil && resp.StatusCode == http.StatusOK {
						var orResp struct {
							Data []struct {
								ID string `json:"id"`
							} `json:"data"`
						}
						if json.NewDecoder(resp.Body).Decode(&orResp) == nil {
							for _, item := range orResp.Data {
								addModel(item.ID, ProviderOpenRouter)
							}
						}
						_ = resp.Body.Close()
					}
				}
			}
		}
	}

	// Se não houver nenhum modelo cadastrado, fornecer openrouter/auto como padrão de segurança
	if len(models) == 0 {
		addModel("openrouter/auto", ProviderOpenRouter)
	}

	return ModelListResponse{
		Object: "list",
		Data:   models,
	}
}
