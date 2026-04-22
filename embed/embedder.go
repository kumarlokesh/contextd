// Package embed provides text-to-vector embedding implementations for contextd.
// Supported backends: Ollama (local) and OpenAI (remote).
package embed

import "context"

// Embedder converts text into a dense float32 vector.
// Implementations must be safe for concurrent use.
type Embedder interface {
	// Embed returns the embedding for the given text.
	Embed(ctx context.Context, text string) ([]float32, error)
	// Close releases any resources held by the embedder (e.g. HTTP clients).
	Close() error
}
