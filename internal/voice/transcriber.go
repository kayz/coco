package voice

import (
	"context"
	"fmt"
)

// Transcriber provides a simple interface for speech-to-text
type Transcriber struct {
	provider Provider
}

// TranscriberConfig holds transcriber configuration
type TranscriberConfig struct {
	Provider string // "system", "openai", "elevenlabs"
	APIKey   string // API key for cloud providers
}

// NewTranscriber creates a new Transcriber
func NewTranscriber(cfg TranscriberConfig) (*Transcriber, error) {
	var provider Provider
	var err error

	switch cfg.Provider {
	case "openai":
		provider, err = NewOpenAIProvider(cfg.APIKey)
	case "elevenlabs":
		provider, err = NewElevenLabsProvider(cfg.APIKey)
	case "system", "":
		provider = NewSystemProvider()
	default:
		return nil, fmt.Errorf("unknown voice provider: %s", cfg.Provider)
	}

	if err != nil {
		return nil, err
	}

	return &Transcriber{provider: provider}, nil
}

// Transcribe converts audio to text
func (t *Transcriber) Transcribe(ctx context.Context, audio []byte) (string, error) {
	return t.provider.SpeechToText(ctx, audio, STTOptions{})
}

// TranscribeWithLanguage converts audio to text with a language hint
func (t *Transcriber) TranscribeWithLanguage(ctx context.Context, audio []byte, language string) (string, error) {
	return t.provider.SpeechToText(ctx, audio, STTOptions{
		Language: language,
	})
}

// ProviderName returns the name of the underlying provider
func (t *Transcriber) ProviderName() string {
	return t.provider.Name()
}
