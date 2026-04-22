package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const openAIEmbedURL = "https://api.openai.com/v1/embeddings"

// OpenAIEmbedder calls the OpenAI Embeddings API.
// The API key is read from the OPENAI_API_KEY environment variable at
// construction time via NewOpenAIEmbedder.
type OpenAIEmbedder struct {
	model      string
	dimensions int
	apiKey     string
	client     *http.Client
}

// NewOpenAIEmbedder creates an OpenAIEmbedder.
// apiKey is the OpenAI API key (typically from os.Getenv("OPENAI_API_KEY")).
// dimensions controls the output vector length (text-embedding-3-* supports
// Matryoshka truncation; pass 0 to use the model default).
func NewOpenAIEmbedder(apiKey, model string, dimensions int) *OpenAIEmbedder {
	return &OpenAIEmbedder{
		model:      model,
		dimensions: dimensions,
		apiKey:     apiKey,
		client:     &http.Client{Timeout: 30 * time.Second},
	}
}

type openAIEmbedRequest struct {
	Model      string `json:"model"`
	Input      string `json:"input"`
	Dimensions int    `json:"dimensions,omitempty"`
}

type openAIEmbedResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

// Embed calls POST /v1/embeddings. On 429 or 5xx it retries up to 3 times
// with exponential back-off (1s, 2s, 4s).
func (e *OpenAIEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	payload := openAIEmbedRequest{Model: e.model, Input: text}
	if e.dimensions > 0 {
		payload.Dimensions = e.dimensions
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	var lastErr error
	backoff := time.Second
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
				backoff *= 2
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost,
			openAIEmbedURL, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+e.apiKey)

		resp, err := e.client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("openai embed: %w", err)
			continue
		}

		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			resp.Body.Close()
			lastErr = fmt.Errorf("openai embed: status %d", resp.StatusCode)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("openai embed: unexpected status %d", resp.StatusCode)
		}

		var result openAIEmbedResponse
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("openai embed: decoding response: %w", err)
		}
		resp.Body.Close()

		if len(result.Data) == 0 || len(result.Data[0].Embedding) == 0 {
			return nil, fmt.Errorf("openai embed: empty embedding returned")
		}
		return result.Data[0].Embedding, nil
	}
	return nil, lastErr
}

// Close is a no-op.
func (e *OpenAIEmbedder) Close() error { return nil }
