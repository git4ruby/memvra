package adapter

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// ollamaAdapter implements LLMAdapter for a local Ollama instance.
type ollamaAdapter struct {
	host       string
	embedModel string
	client     *http.Client
}

// NewOllama creates an Ollama adapter.
func NewOllama(host, embedModel string) LLMAdapter {
	return &ollamaAdapter{
		host:       strings.TrimRight(host, "/"),
		embedModel: embedModel,
		client:     &http.Client{},
	}
}

func (o *ollamaAdapter) Info() ModelInfo {
	return ModelInfo{
		Name:               o.embedModel,
		Provider:           ProviderOllama,
		MaxContextWindow:   32768,
		SupportsStreaming:  true,
		EmbeddingDimension: 384,
	}
}

// ollamaEmbedRequest is the request body for the Ollama embed API.
type ollamaEmbedRequest struct {
	Model  string   `json:"model"`
	Input  []string `json:"input"`
}

// ollamaEmbedResponse is the response from the Ollama embed API.
type ollamaEmbedResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
}

func (o *ollamaAdapter) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	body, err := json.Marshal(ollamaEmbedRequest{
		Model: o.embedModel,
		Input: texts,
	})
	if err != nil {
		return nil, fmt.Errorf("ollama embed marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		o.host+"/api/embed", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("ollama embed request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama embed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama embed: unexpected status %d", resp.StatusCode)
	}

	var result ollamaEmbedResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ollama embed decode: %w", err)
	}

	return result.Embeddings, nil
}

// ollamaChatRequest is the request body for the Ollama chat API.
type ollamaChatRequest struct {
	Model    string              `json:"model"`
	Messages []ollamaChatMessage `json:"messages"`
	Stream   bool                `json:"stream"`
	Options  map[string]any      `json:"options,omitempty"`
}

type ollamaChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ollamaChatChunk is a single streamed response chunk.
type ollamaChatChunk struct {
	Message ollamaChatMessage `json:"message"`
	Done    bool              `json:"done"`
}

func (o *ollamaAdapter) Complete(ctx context.Context, req CompletionRequest) (<-chan StreamChunk, error) {
	model := req.Model
	if model == "" {
		model = o.embedModel
	}
	if model == "" {
		model = "llama3.2"
	}

	messages := []ollamaChatMessage{}
	if req.SystemPrompt != "" {
		messages = append(messages, ollamaChatMessage{Role: "system", Content: req.SystemPrompt})
	}
	if req.Context != "" {
		messages = append(messages, ollamaChatMessage{
			Role:    "system",
			Content: fmt.Sprintf("<context>\n%s\n</context>", req.Context),
		})
	}
	messages = append(messages, ollamaChatMessage{Role: "user", Content: req.UserMessage})

	body, err := json.Marshal(ollamaChatRequest{
		Model:    model,
		Messages: messages,
		Stream:   req.Stream,
		Options: map[string]any{
			"temperature": req.Temperature,
			"num_predict": req.MaxTokens,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("ollama complete marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		o.host+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("ollama complete request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	ch := make(chan StreamChunk, 64)

	go func() {
		defer close(ch)

		resp, err := o.client.Do(httpReq)
		if err != nil {
			ch <- StreamChunk{Error: fmt.Errorf("ollama complete: %w", err)}
			return
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			ch <- StreamChunk{Error: fmt.Errorf("ollama complete: status %d", resp.StatusCode)}
			return
		}

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}
			var chunk ollamaChatChunk
			if err := json.Unmarshal(line, &chunk); err != nil {
				ch <- StreamChunk{Error: fmt.Errorf("ollama stream decode: %w", err)}
				return
			}
			if chunk.Message.Content != "" {
				ch <- StreamChunk{Text: chunk.Message.Content}
			}
			if chunk.Done {
				return
			}
		}
		if err := scanner.Err(); err != nil {
			ch <- StreamChunk{Error: fmt.Errorf("ollama stream scan: %w", err)}
		}
	}()

	return ch, nil
}
