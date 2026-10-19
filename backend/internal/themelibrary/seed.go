package themelibrary

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

//go:embed seed.json
var embeddedSeed []byte

func EmbeddedSeed() (Seed, error) {
	var seed Seed
	if err := json.Unmarshal(embeddedSeed, &seed); err != nil {
		return Seed{}, fmt.Errorf("decode embedded theme library seed: %w", err)
	}
	return seed, nil
}
