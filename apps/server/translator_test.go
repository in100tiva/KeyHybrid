package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
)

func TestParseModelPrefix(t *testing.T) {
	cases := []struct {
		input        string
		wantProvider string
		wantModel    string
	}{
		{"anthropic/claude-3-5-sonnet", ProviderAnthropic, "claude-3-5-sonnet"},
		{"claude/claude-3-opus", ProviderAnthropic, "claude-3-opus"},
		{"gemini/gemini-2.0-flash", ProviderGemini, "gemini-2.0-flash"},
		{"google/gemini-1.5-pro", ProviderGemini, "gemini-1.5-pro"},
		{"groq/llama-3.3-70b-versatile", ProviderGroq, "llama-3.3-70b-versatile"},
		{"openai/gpt-4o", ProviderOpenAI, "gpt-4o"},
		{"openrouter/auto", ProviderOpenRouter, "auto"},
		{"gpt-4o", "", "gpt-4o"},
		{"claude-3-5-sonnet", "", "claude-3-5-sonnet"},
	}

	for _, tc := range cases {
		gotProv, gotMod := parseModelPrefix(tc.input)
		if gotProv != tc.wantProvider || gotMod != tc.wantModel {
			t.Errorf("parseModelPrefix(%q) = (%q, %q), want (%q, %q)",
				tc.input, gotProv, gotMod, tc.wantProvider, tc.wantModel)
		}
	}
}

func TestTranslateOpenAIToAnthropic(t *testing.T) {
	payload := map[string]any{
		"model": "anthropic/claude-3-5-sonnet-20241022",
		"messages": []any{
			map[string]any{"role": "system", "content": "Instrução de sistema 1."},
			map[string]any{"role": "system", "content": "Instrução de sistema 2."},
			map[string]any{"role": "user", "content": "Primeira mensagem."},
			map[string]any{"role": "user", "content": "Segunda mensagem consecutiva do usuário."},
			map[string]any{"role": "assistant", "content": "Resposta do assistente."},
			map[string]any{"role": "user", "content": "Pergunta final."},
		},
		"temperature": 0.5,
		"top_p":       0.9,
		"stop":        []any{"END", "STOP"},
		"stream":      false,
	}

	req, err := translateOpenAIToAnthropic(payload, "claude-3-5-sonnet-20241022")
	if err != nil {
		t.Fatalf("translateOpenAIToAnthropic falhou: %v", err)
	}

	// 1. Modelo deve estar sem prefixo
	if req.Model != "claude-3-5-sonnet-20241022" {
		t.Errorf("Model = %q, want 'claude-3-5-sonnet-20241022'", req.Model)
	}

	// 2. Sistema deve concatenar as mensagens de system
	wantSystem := "Instrução de sistema 1.\n\nInstrução de sistema 2."
	if req.System != wantSystem {
		t.Errorf("System = %q, want %q", req.System, wantSystem)
	}

	// 3. max_tokens padrão de 4096 quando omitido
	if req.MaxTokens != 4096 {
		t.Errorf("MaxTokens = %d, want 4096", req.MaxTokens)
	}

	// 4. Mensagens de mesmo papel consecutivas devem ser unificadas para alternância estrita
	// User (1+2 unificados) -> Assistant -> User
	if len(req.Messages) != 3 {
		t.Fatalf("len(Messages) = %d, want 3", len(req.Messages))
	}
	if req.Messages[0].Role != "user" || !strings.Contains(req.Messages[0].Content.(string), "Segunda mensagem") {
		t.Errorf("Mensagens consecutivas de user não foram unificadas corretamente: %+v", req.Messages[0])
	}
	if req.Messages[1].Role != "assistant" {
		t.Errorf("Messages[1].Role = %q, want assistant", req.Messages[1].Role)
	}
	if req.Messages[2].Role != "user" {
		t.Errorf("Messages[2].Role = %q, want user", req.Messages[2].Role)
	}

	// 5. Parâmetros opcionais
	if req.Temperature == nil || *req.Temperature != 0.5 {
		t.Errorf("Temperature = %v, want 0.5", req.Temperature)
	}
	if req.TopP == nil || *req.TopP != 0.9 {
		t.Errorf("TopP = %v, want 0.9", req.TopP)
	}
	if len(req.StopSequences) != 2 || req.StopSequences[0] != "END" {
		t.Errorf("StopSequences = %v, want [END, STOP]", req.StopSequences)
	}
}

func TestTranslateAnthropicToOpenAI(t *testing.T) {
	antResp := &AnthropicResponse{
		ID:         "msg_01XFDUDYJgAACzvnptvVoYEL",
		Type:       "message",
		Role:       "assistant",
		Model:      "claude-3-5-sonnet-20241022",
		StopReason: "end_turn",
		Content: []AnthropicContentBlock{
			{Type: "text", Text: "Olá! "},
			{Type: "text", Text: "Como posso ajudar você hoje?"},
		},
	}
	antResp.Usage.InputTokens = 25
	antResp.Usage.OutputTokens = 12

	openaiResp := translateAnthropicToOpenAI(antResp, "anthropic/claude-3-5-sonnet-20241022")

	// 1. ID com prefixo chatcmpl-
	if gotID := openaiResp["id"].(string); gotID != "chatcmpl-msg_01XFDUDYJgAACzvnptvVoYEL" {
		t.Errorf("id = %q, want chatcmpl-msg_01XFDUDYJgAACzvnptvVoYEL", gotID)
	}

	// 2. Objeto e Modelo
	if obj := openaiResp["object"].(string); obj != "chat.completion" {
		t.Errorf("object = %q, want chat.completion", obj)
	}
	if mod := openaiResp["model"].(string); mod != "anthropic/claude-3-5-sonnet-20241022" {
		t.Errorf("model = %q, want anthropic/claude-3-5-sonnet-20241022", mod)
	}

	// 3. Choices e Conteúdo concatenado
	choices := openaiResp["choices"].([]map[string]any)
	if len(choices) != 1 {
		t.Fatalf("len(choices) = %d, want 1", len(choices))
	}
	msg := choices[0]["message"].(map[string]any)
	if msg["role"] != "assistant" {
		t.Errorf("role = %v, want assistant", msg["role"])
	}
	wantText := "Olá! Como posso ajudar você hoje?"
	if msg["content"] != wantText {
		t.Errorf("content = %q, want %q", msg["content"], wantText)
	}
	if choices[0]["finish_reason"] != "stop" {
		t.Errorf("finish_reason = %v, want stop", choices[0]["finish_reason"])
	}

	// 4. Usage
	usage := openaiResp["usage"].(map[string]any)
	if usage["prompt_tokens"] != 25 || usage["completion_tokens"] != 12 || usage["total_tokens"] != 37 {
		t.Errorf("usage inválido: %+v", usage)
	}
}

func TestPipeAnthropicStreamToOpenAI(t *testing.T) {
	ssePayload := `event: message_start
data: {"type":"message_start","message":{"id":"msg_stream_01","type":"message","role":"assistant","model":"claude-3-5-sonnet-20241022"}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Bom"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" dia!"}}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}

event: message_stop
data: {"type":"message_stop"}
`

	rec := httptest.NewRecorder()
	ctx := context.Background()
	err := pipeAnthropicStreamToOpenAI(ctx, rec, strings.NewReader(ssePayload), "anthropic/claude-3-5-sonnet-20241022")
	if err != nil {
		t.Fatalf("pipeAnthropicStreamToOpenAI falhou: %v", err)
	}

	out := rec.Body.String()

	// Deve conter content-type text/event-stream
	if !strings.Contains(rec.Header().Get("Content-Type"), "text/event-stream") {
		t.Errorf("Content-Type = %q, want text/event-stream", rec.Header().Get("Content-Type"))
	}

	// Deve conter chunks com os deltas de texto
	if !strings.Contains(out, `"delta":{"content":"Bom"}`) {
		t.Errorf("Saída não contém primeiro delta de texto: %s", out)
	}
	if !strings.Contains(out, `"delta":{"content":" dia!"}`) {
		t.Errorf("Saída não contém segundo delta de texto: %s", out)
	}
	if !strings.Contains(out, `"finish_reason":"stop"`) {
		t.Errorf("Saída não contém finish_reason stop: %s", out)
	}
	if !strings.Contains(out, "data: [DONE]") {
		t.Errorf("Saída não termina com [DONE]: %s", out)
	}
}

func TestResolveAIProvider_Precedence(t *testing.T) {
	col := core.NewBaseCollection("vault_keys")
	rOpenRouter := core.NewRecord(col)
	rOpenRouter.Set("alias", "openrouter")
	rOpenRouter.Set("provider", "openrouter")
	rOpenRouter.Set("active", true)

	rAnthropic := core.NewRecord(col)
	rAnthropic.Set("alias", "anthropic")
	rAnthropic.Set("provider", "anthropic")
	rAnthropic.Set("active", false)

	rGroq := core.NewRecord(col)
	rGroq.Set("alias", "groq")
	rGroq.Set("provider", "groq")
	rGroq.Set("active", false)

	rGemini := core.NewRecord(col)
	rGemini.Set("alias", "gemini")
	rGemini.Set("provider", "gemini")
	rGemini.Set("active", false)

	allRecords := []*core.Record{rOpenRouter, rAnthropic, rGroq, rGemini}

	// 1. Header X-Provider tem precedência máxima
	rec, cleanMod, prov := resolveAIProvider("", "anthropic", "gpt-4o", allRecords)
	if rec != rAnthropic || prov != ProviderAnthropic {
		t.Errorf("Header X-Provider não selecionou Anthropic: rec=%v, prov=%s", rec, prov)
	}
	if cleanMod != "gpt-4o" {
		t.Errorf("cleanMod = %q, want gpt-4o", cleanMod)
	}

	// 2. Prefixo do model id seleciona o provedor correspondente
	rec, cleanMod, prov = resolveAIProvider("", "", "groq/llama-3.3-70b-versatile", allRecords)
	if rec != rGroq || prov != ProviderGroq {
		t.Errorf("Prefixo groq/ não selecionou Groq: rec=%v, prov=%s", rec, prov)
	}
	if cleanMod != "llama-3.3-70b-versatile" {
		t.Errorf("cleanMod = %q, want llama-3.3-70b-versatile", cleanMod)
	}

	// 3. Virtual token / requestedAlias kh-gemini
	rec, cleanMod, prov = resolveAIProvider("gemini", "", "gemini-2.0-flash", allRecords)
	if rec != rGemini || prov != ProviderGemini {
		t.Errorf("requestedAlias gemini não selecionou Gemini: rec=%v, prov=%s", rec, prov)
	}
	if cleanMod != "gemini-2.0-flash" {
		t.Errorf("cleanMod = %q, want gemini-2.0-flash", cleanMod)
	}

	// 4. Sem prefixo e sem header: fallback para a chave ativa (OpenRouter) mantendo comportamento default
	rec, cleanMod, prov = resolveAIProvider("", "", "openrouter/auto", allRecords)
	if rec != rOpenRouter || prov != ProviderOpenRouter {
		t.Errorf("Fallback default não selecionou chave ativa: rec=%v, prov=%s", rec, prov)
	}

	// 5. Modelo com prefixo anthropic/... mas SEM chave anthropic cadastrada:
	// Deve fazer fallback para a chave ativa para não quebrar compatibilidade
	recOnlyOR, _, provOnlyOR := resolveAIProvider("", "", "anthropic/claude-3-5-sonnet", []*core.Record{rOpenRouter})
	if recOnlyOR != rOpenRouter || provOnlyOR != ProviderOpenRouter {
		t.Errorf("Fallback sem chave anthropic falhou: rec=%v, prov=%s", recOnlyOR, provOnlyOR)
	}

	// 6. Alias explícito inexistente não cai na chave ativa (#43)
	if rec, _, prov = resolveAIProvider("errado", "", "gpt-4o", allRecords); rec != nil || prov != "" {
		t.Errorf("Alias inexistente caiu em fallback: rec=%v, prov=%s", rec, prov)
	}
	// 7. kh-default continua indo para a chave ativa
	if rec, _, _ = resolveAIProvider("default", "", "gpt-4o", allRecords); rec != rOpenRouter {
		t.Errorf("default não selecionou a chave ativa: rec=%v", rec)
	}
}

func TestAggregateEnabledModels(t *testing.T) {
	col := core.NewBaseCollection("vault_keys")
	rAnthropic := core.NewRecord(col)
	rAnthropic.Set("provider", "anthropic")
	rAnthropic.Set("active", false)

	rGemini := core.NewRecord(col)
	rGemini.Set("provider", "gemini")
	rGemini.Set("active", true) // Ativa: deve exportar modelos com e sem prefixo

	rGroq := core.NewRecord(col)
	rGroq.Set("provider", "groq")
	rGroq.Set("active", false)

	records := []*core.Record{rAnthropic, rGemini, rGroq}

	resp := aggregateEnabledModels(context.Background(), records)
	if resp.Object != "list" {
		t.Errorf("Object = %q, want list", resp.Object)
	}

	foundMap := make(map[string]bool)
	for _, m := range resp.Data {
		foundMap[m.ID] = true
	}

	// Deve conter modelos da Anthropic com prefixo
	if !foundMap["anthropic/claude-3-5-sonnet"] {
		t.Errorf("Modelos agregados não contêm anthropic/claude-3-5-sonnet")
	}

	// Deve conter modelos do Gemini com prefixo E sem prefixo (pois é a ativa)
	if !foundMap["gemini/gemini-2.0-flash"] {
		t.Errorf("Modelos agregados não contêm gemini/gemini-2.0-flash")
	}
	if !foundMap["gemini-2.0-flash"] {
		t.Errorf("Modelos agregados não contêm modelo default ativo gemini-2.0-flash")
	}

	// Deve conter modelos do Groq
	if !foundMap["groq/llama-3.3-70b-versatile"] {
		t.Errorf("Modelos agregados não contêm groq/llama-3.3-70b-versatile")
	}
}

func TestAnthropicEndToEndContract(t *testing.T) {
	// Cria servidor falso simulando a API da Anthropic
	var receivedAPIKey, receivedVersion string
	var receivedBody AnthropicRequest

	anthropicServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAPIKey = r.Header.Get("x-api-key")
		receivedVersion = r.Header.Get("anthropic-version")
		_ = json.NewDecoder(r.Body).Decode(&receivedBody)

		resp := AnthropicResponse{
			ID:         "msg_test_e2e_01",
			Type:       "message",
			Role:       "assistant",
			Model:      receivedBody.Model,
			StopReason: "end_turn",
			Content: []AnthropicContentBlock{
				{Type: "text", Text: "Resposta contratual da Anthropic"},
			},
		}
		resp.Usage.InputTokens = 10
		resp.Usage.OutputTokens = 8
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer anthropicServer.Close()

	// Simula chamada OpenAI do cliente para o gateway
	openaiPayload := map[string]any{
		"model": "anthropic/claude-3-5-sonnet",
		"messages": []any{
			map[string]any{"role": "system", "content": "Seja direto."},
			map[string]any{"role": "user", "content": "Olá Anthropic!"},
		},
	}

	// Traduz payload
	antReq, err := translateOpenAIToAnthropic(openaiPayload, "claude-3-5-sonnet")
	if err != nil {
		t.Fatalf("translateOpenAIToAnthropic falhou: %v", err)
	}

	bodyBytes, _ := json.Marshal(antReq)
	req, _ := http.NewRequest(http.MethodPost, anthropicServer.URL+"/v1/messages", bytes.NewReader(bodyBytes))
	req.Header.Set("x-api-key", "sk-ant-test-chave-123")
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	httpResp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Erro ao chamar mock Anthropic: %v", err)
	}
	defer httpResp.Body.Close()

	if receivedAPIKey != "sk-ant-test-chave-123" {
		t.Errorf("Header x-api-key recebido = %q, want sk-ant-test-chave-123", receivedAPIKey)
	}
	if receivedVersion != "2023-06-01" {
		t.Errorf("Header anthropic-version recebido = %q, want 2023-06-01", receivedVersion)
	}
	if receivedBody.System != "Seja direto." {
		t.Errorf("System prompt = %q, want 'Seja direto.'", receivedBody.System)
	}

	var upstreamResp AnthropicResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&upstreamResp); err != nil {
		t.Fatalf("Erro ao decodificar mock Anthropic: %v", err)
	}

	finalOpenAI := translateAnthropicToOpenAI(&upstreamResp, "anthropic/claude-3-5-sonnet")
	choices := finalOpenAI["choices"].([]map[string]any)
	msg := choices[0]["message"].(map[string]any)
	if msg["content"] != "Resposta contratual da Anthropic" {
		t.Errorf("Resposta final OpenAI = %v, want 'Resposta contratual da Anthropic'", msg["content"])
	}
}
