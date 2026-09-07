package codexintegration

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"path/filepath"

	"github.com/abyssbugg/ant-router/internal/codexownership"
)

type Action string

const (
	ActionEnable  Action = "enable"
	ActionDisable Action = "disable"
	ActionRestore Action = "restore"
)

type ChangeKind string

const (
	ChangeCreate ChangeKind = "create"
	ChangeUpdate ChangeKind = "update"
	ChangeRemove ChangeKind = "remove"
)

type FileObservation struct {
	Exists bool
	SHA256 string
}
type Change struct {
	Path string
	Kind ChangeKind
}

type PlanInput struct {
	Action                                  Action
	ProfilePath, CatalogPath, RouterctlPath string
	CodexVersion                            string
	ConfigGeneration                        uint64
	CurrentProfile, CurrentCatalog          FileObservation
	OwnershipGeneration                     string
	DesiredProfile, DesiredCatalog          []byte
}

type Plan struct {
	input            PlanInput
	action           Action
	digest           string
	changes          []Change
	profile, catalog []byte
}

func NewPlan(input PlanInput) (Plan, error) {
	if !validatePlanInput(input) {
		return Plan{}, errors.New("invalid Codex integration plan")
	}
	changes := make([]Change, 0, 2)
	appendChange := func(path string, current FileObservation, desired []byte) {
		if kind, changed := plannedChange(path, current, desired, input.Action); changed {
			changes = append(changes, Change{Path: path, Kind: kind})
		}
	}
	if input.Action == ActionDisable {
		appendChange(input.ProfilePath, input.CurrentProfile, nil)
		appendChange(input.CatalogPath, input.CurrentCatalog, nil)
	} else {
		appendChange(input.CatalogPath, input.CurrentCatalog, input.DesiredCatalog)
		appendChange(input.ProfilePath, input.CurrentProfile, input.DesiredProfile)
	}
	input.DesiredProfile = append([]byte(nil), input.DesiredProfile...)
	input.DesiredCatalog = append([]byte(nil), input.DesiredCatalog...)
	return Plan{input: input, action: input.Action, digest: planDigest(input), changes: changes, profile: append([]byte(nil), input.DesiredProfile...), catalog: append([]byte(nil), input.DesiredCatalog...)}, nil
}

func (p Plan) Action() Action    { return p.action }
func (p Plan) Digest() string    { return p.digest }
func (p Plan) Changes() []Change { return append([]Change(nil), p.changes...) }
func (p Plan) Profile() []byte   { return append([]byte(nil), p.profile...) }
func (p Plan) Catalog() []byte   { return append([]byte(nil), p.catalog...) }

func validatePlanInput(input PlanInput) bool {
	if input.Action != ActionEnable && input.Action != ActionDisable && input.Action != ActionRestore {
		return false
	}
	if !filepath.IsAbs(input.ProfilePath) || !filepath.IsAbs(input.CatalogPath) || !filepath.IsAbs(input.RouterctlPath) || input.CodexVersion == "" {
		return false
	}
	if !validObservation(input.CurrentProfile) || !validObservation(input.CurrentCatalog) || (input.OwnershipGeneration != "" && !validDigest(input.OwnershipGeneration)) {
		return false
	}
	switch input.Action {
	case ActionEnable:
		return input.ConfigGeneration > 0 && len(input.DesiredProfile) > 0 && len(input.DesiredProfile) <= codexownership.MaxProfileBytes && len(input.DesiredCatalog) > 0 && len(input.DesiredCatalog) <= codexownership.MaxCatalogBytes
	case ActionDisable:
		return len(input.DesiredProfile) == 0 && len(input.DesiredCatalog) == 0 && (input.OwnershipGeneration != "" || !input.CurrentProfile.Exists && !input.CurrentCatalog.Exists)
	case ActionRestore:
		return input.OwnershipGeneration != "" && len(input.DesiredProfile) > 0 && len(input.DesiredProfile) <= codexownership.MaxProfileBytes && len(input.DesiredCatalog) > 0 && len(input.DesiredCatalog) <= codexownership.MaxCatalogBytes
	default:
		return false
	}
}

func validObservation(value FileObservation) bool {
	if !value.Exists {
		return value.SHA256 == ""
	}
	return validDigest(value.SHA256)
}

func validDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func plannedChange(path string, current FileObservation, desired []byte, action Action) (ChangeKind, bool) {
	if action == ActionDisable {
		if current.Exists {
			return ChangeRemove, true
		}
		return "", false
	}
	digest := sha256.Sum256(desired)
	if !current.Exists {
		return ChangeCreate, true
	}
	if current.SHA256 != hex.EncodeToString(digest[:]) {
		return ChangeUpdate, true
	}
	return "", false
}

func planDigest(input PlanInput) string {
	hash := sha256.New()
	write := func(value []byte) {
		var size [8]byte
		binary.BigEndian.PutUint64(size[:], uint64(len(value)))
		_, _ = hash.Write(size[:])
		_, _ = hash.Write(value)
	}
	write([]byte("ant-router-codex-plan-v1"))
	for _, value := range []string{string(input.Action), input.ProfilePath, input.CatalogPath, input.RouterctlPath, input.CodexVersion, input.CurrentProfile.SHA256, input.CurrentCatalog.SHA256, input.OwnershipGeneration} {
		write([]byte(value))
	}
	var generation [8]byte
	binary.BigEndian.PutUint64(generation[:], input.ConfigGeneration)
	write(generation[:])
	if input.CurrentProfile.Exists {
		write([]byte{1})
	} else {
		write([]byte{0})
	}
	if input.CurrentCatalog.Exists {
		write([]byte{1})
	} else {
		write([]byte{0})
	}
	write(input.DesiredProfile)
	write(input.DesiredCatalog)
	return hex.EncodeToString(hash.Sum(nil))
}
