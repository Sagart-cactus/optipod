package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/optipod/optipod/test/e2e/framework"
)

// Fixture changes are infrequent. Reuse its already installed image during
// controller-only edits, retaining a content-based check instead of timestamps.
func fixtureImage(ctx context.Context, b *framework.ImageBuilder, root, installed string) (string, error) {
	h := sha256.New()
	for _, name := range []string{"go.mod",
		"go.sum",
		"test/e2e/metricsfixture/main.go",
		"test/e2e/metricsfixture/injector.go",
		"test/e2e/metricsfixture/Dockerfile"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			return "", err
		}
		h.Write([]byte(name))
		h.Write(data)
	}
	hash := hex.EncodeToString(h.Sum(nil))
	path := filepath.Join(root, ".dev", "fixture-image.json")
	var cached struct{ Hash, Image string }
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &cached)
	}
	if cached.Hash == hash && cached.Image == installed {
		return installed, nil
	}
	image, err := b.BuildFixtureImage(ctx)
	if err != nil {
		return "", err
	}
	cached.Hash, cached.Image = hash, image.Ref
	data, _ := json.Marshal(cached)
	if err := os.WriteFile(path, data, 0600); err != nil {
		return "", err
	}
	return image.Ref, nil
}
