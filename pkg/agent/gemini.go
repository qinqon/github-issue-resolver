package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"cloud.google.com/go/vertexai/genai"
)

// GeminiClient reviews PR diffs using Gemini.
type GeminiClient interface {
	ReviewPRDiff(ctx context.Context, diff, prompt string) ([]ReviewSuggestion, error)
}

// VertexGeminiClient implements GeminiClient using Google Cloud Vertex AI.
type VertexGeminiClient struct {
	projectID string
	region    string
	model     string
}

// NewVertexGeminiClient creates a new Gemini client using Vertex AI.
func NewVertexGeminiClient(projectID, region, model string) (*VertexGeminiClient, error) {
	if projectID == "" || region == "" || model == "" {
		return nil, fmt.Errorf("projectID, region, and model are required")
	}
	return &VertexGeminiClient{
		projectID: projectID,
		region:    region,
		model:     model,
	}, nil
}

// ReviewPRDiff sends a PR diff to Gemini for review and returns suggestions.
func (g *VertexGeminiClient) ReviewPRDiff(ctx context.Context, diff, prompt string) ([]ReviewSuggestion, error) {
	if diff == "" {
		return []ReviewSuggestion{}, nil
	}

	client, err := genai.NewClient(ctx, g.projectID, g.region)
	if err != nil {
		return nil, fmt.Errorf("creating Gemini client: %w", err)
	}
	defer client.Close()

	model := client.GenerativeModel(g.model)
	model.SetTemperature(0.2) // Lower temperature for more focused, deterministic reviews

	fullPrompt := fmt.Sprintf("%s\n\nDiff:\n```\n%s\n```\n\nProvide your response as a JSON array only, no additional text.", prompt, diff)

	resp, err := model.GenerateContent(ctx, genai.Text(fullPrompt))
	if err != nil {
		return nil, fmt.Errorf("generating content: %w", err)
	}

	if len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
		return []ReviewSuggestion{}, nil
	}

	// Extract text from response
	var responseText string
	for _, part := range resp.Candidates[0].Content.Parts {
		if textPart, ok := part.(genai.Text); ok {
			responseText += string(textPart)
		}
	}

	// Parse JSON response
	responseText = strings.TrimSpace(responseText)

	// Handle markdown code blocks
	if strings.HasPrefix(responseText, "```json") {
		responseText = strings.TrimPrefix(responseText, "```json")
		responseText = strings.TrimSuffix(responseText, "```")
		responseText = strings.TrimSpace(responseText)
	} else if strings.HasPrefix(responseText, "```") {
		responseText = strings.TrimPrefix(responseText, "```")
		responseText = strings.TrimSuffix(responseText, "```")
		responseText = strings.TrimSpace(responseText)
	}

	var suggestions []ReviewSuggestion
	if err := json.Unmarshal([]byte(responseText), &suggestions); err != nil {
		return nil, fmt.Errorf("parsing Gemini response as JSON: %w (response: %s)", err, responseText)
	}

	return suggestions, nil
}

// DefaultGeminiReviewPrompt is the default prompt used for Gemini code reviews.
const DefaultGeminiReviewPrompt = `You are reviewing a pull request. Analyze the diff and identify:
- Bugs or logic errors
- Missing error handling
- Potential nil pointer dereferences
- Security concerns (SQL injection, XSS, command injection)
- Race conditions or concurrency issues

For each issue, provide:
1. File path (relative path from the diff)
2. Line number (in the new version, from the diff context)
3. Severity: "error" (must fix), "warning" (should fix), or "info" (suggestion)
4. Clear description of the issue and suggested fix

Focus on correctness and safety. Ignore style unless it affects correctness.

Output format: JSON array of objects with fields: file_path, line, severity, message.
Example: [{"file_path": "pkg/example.go", "line": 42, "severity": "error", "message": "Potential nil pointer dereference"}]

If no issues are found, return an empty array: []`
