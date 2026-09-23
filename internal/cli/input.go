package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/antaeusio/antaeus/evaluator"
	"github.com/antaeusio/antaeus/internal/jsonvalue"
)

func loadCanonicalInput(path string) (json.RawMessage, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	defer file.Close()
	source, err := io.ReadAll(io.LimitReader(file, evaluator.MaxInputBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}
	if len(source) > evaluator.MaxInputBytes {
		return nil, fmt.Errorf("must not exceed %d bytes", evaluator.MaxInputBytes)
	}
	return jsonvalue.CanonicalObject(source)
}
