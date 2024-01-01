package gemini

import (
	"context"
	"fmt"
	"log"

	"github.com/google/generative-ai-go/genai"
	"google.golang.org/api/option"
)

type GenerateTextConfig struct {
	APIKey    string
	ModelName string // empty for "gemini-pro"
	Prompt    string
}

func GenerateText(ctx context.Context, cfg GenerateTextConfig) (string, error) {
	// Access your API key as an environment variable (see "Set up your API key" above)
	client, err := genai.NewClient(ctx, option.WithAPIKey(cfg.APIKey))
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	// For text-only input, use the gemini-pro model
	modelName := cfg.ModelName
	if modelName == "" {
		modelName = "gemini-pro"
	}
	model := client.GenerativeModel(modelName)
	resp, err := model.GenerateContent(ctx, genai.Text(cfg.Prompt))
	if err != nil {
		return "", fmt.Errorf("failed to generate text: %w", err)
	}
	if len(resp.Candidates) == 0 {
		return "", fmt.Errorf("no candidate in response")
	}
	if resp.Candidates[0].Content == nil {
		return "", fmt.Errorf("no content in the first candidate")
	}
	result := ""
	for _, part := range resp.Candidates[0].Content.Parts {
		switch p := part.(type) {
		case genai.Text:
			result += string(p)
		case genai.Blob:
			result += fmt.Sprintf("<%d bytes %s data>", len(p.Data), p.MIMEType)
		default:
			result += fmt.Sprintf("<unknown part type %T, value: %+v>", p, p)
		}
	}
	return result, nil
}
