package agent

import (
	"context"
	"testing"
)

// MockGeminiClient is a mock implementation of GeminiClient for testing.
type MockGeminiClient struct {
	ReviewFunc func(ctx context.Context, diff, prompt string) ([]ReviewSuggestion, error)
}

func (m *MockGeminiClient) ReviewPRDiff(ctx context.Context, diff, prompt string) ([]ReviewSuggestion, error) {
	if m.ReviewFunc != nil {
		return m.ReviewFunc(ctx, diff, prompt)
	}
	return []ReviewSuggestion{}, nil
}

func TestNewVertexGeminiClient_RequiresParameters(t *testing.T) {
	tests := []struct {
		name      string
		projectID string
		region    string
		model     string
		wantErr   bool
	}{
		{"valid", "project", "us-central1", "gemini-2.0-flash-exp", false},
		{"missing project", "", "us-central1", "gemini-2.0-flash-exp", true},
		{"missing region", "project", "", "gemini-2.0-flash-exp", true},
		{"missing model", "project", "us-central1", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewVertexGeminiClient(tt.projectID, tt.region, tt.model)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewVertexGeminiClient() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestMockGeminiClient_Success(t *testing.T) {
	mock := &MockGeminiClient{
		ReviewFunc: func(ctx context.Context, diff, prompt string) ([]ReviewSuggestion, error) {
			return []ReviewSuggestion{
				{FilePath: "pkg/test.go", Line: 10, Severity: "error", Message: "Test error"},
				{FilePath: "pkg/test.go", Line: 20, Severity: "warning", Message: "Test warning"},
			}, nil
		},
	}

	suggestions, err := mock.ReviewPRDiff(context.Background(), "diff", "prompt")
	if err != nil {
		t.Fatalf("ReviewPRDiff() error = %v", err)
	}

	if len(suggestions) != 2 {
		t.Errorf("got %d suggestions, want 2", len(suggestions))
	}

	if suggestions[0].Severity != "error" {
		t.Errorf("first suggestion severity = %s, want error", suggestions[0].Severity)
	}
}

func TestMockGeminiClient_EmptyDiff(t *testing.T) {
	mock := &MockGeminiClient{
		ReviewFunc: func(ctx context.Context, diff, prompt string) ([]ReviewSuggestion, error) {
			if diff == "" {
				return []ReviewSuggestion{}, nil
			}
			return []ReviewSuggestion{{FilePath: "test", Line: 1, Severity: "info", Message: "msg"}}, nil
		},
	}

	suggestions, err := mock.ReviewPRDiff(context.Background(), "", "prompt")
	if err != nil {
		t.Fatalf("ReviewPRDiff() error = %v", err)
	}

	if len(suggestions) != 0 {
		t.Errorf("got %d suggestions for empty diff, want 0", len(suggestions))
	}
}
