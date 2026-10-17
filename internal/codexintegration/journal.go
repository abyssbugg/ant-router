package codexintegration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"

	"github.com/abyssbugg/ant-router/internal/canonicaljson"
	"github.com/abyssbugg/ant-router/internal/codexownership"
)

// Version 2 records explicit base/target file existence; version 1 journals
// lack it and are rejected rather than guessed.
const pendingSchemaVersion = 2
const maxPendingBytes = 64 << 10

type pendingTransaction struct {
	SchemaVersion       int    `json:"schema_version"`
	Action              Action `json:"action"`
	TargetGeneration    string `json:"target_generation,omitempty"`
	BaseGeneration      string `json:"base_generation,omitempty"`
	ProfilePath         string `json:"profile_path"`
	CatalogPath         string `json:"catalog_path"`
	BaseProfileExists   bool   `json:"base_profile_exists"`
	BaseCatalogExists   bool   `json:"base_catalog_exists"`
	TargetProfileExists bool   `json:"target_profile_exists"`
	TargetCatalogExists bool   `json:"target_catalog_exists"`
}

func validPending(pending pendingTransaction) bool {
	if pending.SchemaVersion != pendingSchemaVersion || !filepath.IsAbs(pending.ProfilePath) || filepath.Clean(pending.ProfilePath) != pending.ProfilePath || !filepath.IsAbs(pending.CatalogPath) || filepath.Clean(pending.CatalogPath) != pending.CatalogPath || pending.ProfilePath == pending.CatalogPath {
		return false
	}
	if pending.TargetGeneration != "" && !validDigest(pending.TargetGeneration) {
		return false
	}
	if pending.BaseGeneration != "" && !validDigest(pending.BaseGeneration) {
		return false
	}
	switch pending.Action {
	case ActionEnable:
		if pending.TargetGeneration == "" || pending.TargetGeneration == pending.BaseGeneration {
			return false
		}
		return pending.TargetProfileExists && pending.TargetCatalogExists
	case ActionRestore:
		// Restore may republish the current owned generation after disable removed its files.
		if pending.TargetGeneration == "" || pending.BaseGeneration == "" {
			return false
		}
		return pending.TargetProfileExists && pending.TargetCatalogExists
	case ActionDisable:
		// Disable targets absence; recovery removes both files regardless of base.
		return pending.TargetGeneration == "" && pending.BaseGeneration != "" && !pending.TargetProfileExists && !pending.TargetCatalogExists
	default:
		return false
	}
}

func (manager *Manager) pendingPath() string {
	return filepath.Join(manager.planner.options.StateRoot, "codex", "ant-router", "pending.json")
}

func (manager *Manager) writePending(ctx context.Context, pending pendingTransaction) error {
	if !validPending(pending) {
		return errors.New("invalid Codex pending transaction")
	}
	data, err := json.MarshalIndent(pending, "", "  ")
	if err != nil {
		return errors.New("Codex pending transaction encoding failed")
	}
	data = append(data, '\n')
	if len(data) > maxPendingBytes {
		return errors.New("Codex pending transaction is too large")
	}
	_, err = manager.write(ctx, manager.pendingPath(), data, 0o600)
	if err != nil {
		return errors.New("Codex pending transaction write failed")
	}
	if err := codexownership.SecureFile(manager.pendingPath()); err != nil {
		return errors.New("Codex pending transaction unavailable")
	}
	return nil
}

func (manager *Manager) readPending(ctx context.Context) (pendingTransaction, bool, error) {
	observation, data, err := observeFile(ctx, manager.pendingPath(), maxPendingBytes)
	if err != nil {
		return pendingTransaction{}, false, err
	}
	if !observation.Exists {
		return pendingTransaction{}, false, nil
	}
	pending, err := decodePending(data)
	if err != nil {
		return pendingTransaction{}, false, err
	}
	return pending, true, nil
}

func decodePending(data []byte) (pendingTransaction, error) {
	if len(data) == 0 || len(data) > maxPendingBytes {
		return pendingTransaction{}, errors.New("invalid Codex pending transaction")
	}
	if _, err := canonicaljson.Parse(data, canonicaljson.DefaultLimits()); err != nil {
		return pendingTransaction{}, errors.New("invalid Codex pending transaction")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var pending pendingTransaction
	if err := decoder.Decode(&pending); err != nil {
		return pendingTransaction{}, errors.New("invalid Codex pending transaction")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return pendingTransaction{}, errors.New("invalid Codex pending transaction")
	}
	if !validPending(pending) {
		return pendingTransaction{}, errors.New("invalid Codex pending transaction")
	}
	return pending, nil
}

func (manager *Manager) clearPending() error {
	_, err := manager.remove(context.Background(), manager.pendingPath())
	if err != nil {
		return errors.New("Codex pending transaction removal failed")
	}
	return nil
}
