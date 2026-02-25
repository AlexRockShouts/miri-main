package memory

import (
	"context"
	"fmt"
	"miri-main/src/internal/storage"
)

func SaveFact(ctx context.Context, st *storage.Storage, fact string) (string, error) {
	if fact == "" {
		return "", fmt.Errorf("fact cannot be empty")
	}

	err := st.AppendToMemory(fact)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("Fact saved to memory.md: %s", fact), nil
}
