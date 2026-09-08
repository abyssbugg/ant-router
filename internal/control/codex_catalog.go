package control

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
)

const MaxCodexCatalogBytes = 4 << 20
const MaxCodexCatalogResponseBytes int64 = 2*MaxCodexCatalogBytes + 64<<10

type CodexCatalogProvider interface {
	CodexCatalog(context.Context) (CodexCatalog, error)
}

type CodexCatalog struct {
	Generation uint64
	JSON       []byte
}

func NewCodexCatalog(generation uint64, data []byte) (CodexCatalog, error) {
	if generation == 0 || len(data) == 0 || len(data) > MaxCodexCatalogBytes || !json.Valid(data) {
		return CodexCatalog{}, errors.New("codex catalog is invalid")
	}
	return CodexCatalog{Generation: generation, JSON: append([]byte(nil), data...)}, nil
}

func (c CodexCatalog) clone() CodexCatalog {
	return CodexCatalog{Generation: c.Generation, JSON: append([]byte(nil), c.JSON...)}
}

func (c CodexCatalog) response() CodexCatalogResponse {
	digest := sha256.Sum256(c.JSON)
	return CodexCatalogResponse{APIVersion: APIVersion, Generation: c.Generation, Catalog: string(c.JSON), SHA256: hex.EncodeToString(digest[:])}
}

type CodexCatalogResponse struct {
	APIVersion string `json:"api_version"`
	Generation uint64 `json:"generation"`
	Catalog    string `json:"catalog"`
	SHA256     string `json:"sha256"`
}

func validateCodexCatalogResponse(response CodexCatalogResponse) error {
	data := []byte(response.Catalog)
	if response.APIVersion != APIVersion || response.Generation == 0 || len(data) == 0 || len(data) > MaxCodexCatalogBytes || !json.Valid(data) {
		return errors.New("Codex catalog response is invalid")
	}
	digest := sha256.Sum256(data)
	if response.SHA256 != hex.EncodeToString(digest[:]) {
		return errors.New("Codex catalog response is invalid")
	}
	return nil
}
