package gemini

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/google/generative-ai-go/genai"
	"github.com/zjx20/hcfy-gemini/util/httpclient"
	"google.golang.org/api/option"
	"google.golang.org/api/transport/http"
)

type GenerateTextConfig struct {
	APIKey    string
	ModelName string // empty for "gemini-pro"
	Prompt    string
}

func GenerateText(ctx context.Context, cfg GenerateTextConfig) (string, error) {
	c := httpclient.CustomPingInterval(15 * time.Second)
	apiTrans, err := http.NewTransport(ctx, c.Transport, option.WithAPIKey(cfg.APIKey))
	if err != nil {
		return "", fmt.Errorf("failed to create API transport: %w", err)
	}
	c.Transport = apiTrans

	client, err := genai.NewClient(ctx, option.WithHTTPClient(c), option.WithAPIKey(cfg.APIKey))
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
