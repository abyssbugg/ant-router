// Package codexownership stores Ant-owned Codex integration generations.
package codexownership

import (
	"errors"
	"time"
)

const (
	MaxProfileBytes  = 64 << 10
	MaxCatalogBytes  = 4 << 20
	MaxManifestBytes = 512 << 10
	MaxGenerations   = 10
)

type Input struct {
	ProfilePath   string
	CatalogPath   string
	Profile       []byte
	Catalog       []byte
	CodexVersion  string
	RouterctlPath string
	CreatedAt     time.Time
}

type Generation struct {
	ID            string    `json:"id"`
	ProfilePath   string    `json:"profile_path"`
	CatalogPath   string    `json:"catalog_path"`
	CodexVersion  string    `json:"codex_version"`
	RouterctlPath string    `json:"routerctl_path"`
	CreatedAt     time.Time `json:"created_at"`
	Profile       []byte    `json:"-"`
	Catalog       []byte    `json:"-"`
}

type Error struct {
	committed bool
	err       error
}

func (err *Error) Error() string { return "Codex ownership operation failed" }
func (err *Error) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.err
}
func Committed(err error) bool {
	var ownershipError *Error
	return errors.As(err, &ownershipError) && ownershipError.committed
}

func committedError(err error) error { return &Error{committed: true, err: err} }

type Manifest struct {
	SchemaVersion int    `json:"schema_version"`
	Current       string `json:"current"`
	Previous      string `json:"previous,omitempty"`
	ProfilePath   string `json:"profile_path"`
	CatalogPath   string `json:"catalog_path"`
}
