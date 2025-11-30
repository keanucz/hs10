//go:build !local_llm

package agents

import (
	"context"
	"fmt"
)

// LocalLLM is a stub when local llama support is disabled.
type LocalLLM struct{}

func getLocalLLM() (*LocalLLM, error) {
	return nil, nil
}

func (l *LocalLLM) Generate(_ context.Context, _, _, _ string) (string, error) {
	return "", fmt.Errorf("local-llm: support not built (compile with -tags local_llm)")
}
