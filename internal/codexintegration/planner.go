package codexintegration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"

	"github.com/abyssbugg/ant-router/internal/codexownership"
	"github.com/abyssbugg/ant-router/internal/codexprofile"
	"github.com/abyssbugg/ant-router/internal/codexversion"
	"github.com/abyssbugg/ant-router/internal/control"
)

type CatalogSource interface {
	CodexCatalog(context.Context) (control.CodexCatalogResponse, error)
}
type VersionSource interface {
	Version(context.Context, bool) (codexversion.Version, error)
}
type OwnershipSource interface {
	Current(context.Context) (codexownership.Manifest, codexownership.Generation, error)
	Generation(context.Context, string) (codexownership.Generation, error)
}

type PlannerOptions struct {
	CodexHome, StateRoot, RouterctlPath, BaseURL, Instance, RuntimeRoot string
	AllowUntested                                                       bool
}

type Planner struct {
	options   PlannerOptions
	catalog   CatalogSource
	versions  VersionSource
	ownership OwnershipSource
}

func NewPlanner(options PlannerOptions, catalog CatalogSource, versions VersionSource, ownership OwnershipSource) (*Planner, error) {
	if !filepath.IsAbs(options.CodexHome) || !filepath.IsAbs(options.StateRoot) || !filepath.IsAbs(options.RouterctlPath) || nilInterface(catalog) || nilInterface(versions) || nilInterface(ownership) {
		return nil, errors.New("invalid Codex planner")
	}
	return &Planner{options: options, catalog: catalog, versions: versions, ownership: ownership}, nil
}

func (planner *Planner) PlanEnable(ctx context.Context) (Plan, error) {
	if planner == nil || ctx == nil {
		return Plan{}, errors.New("invalid Codex plan request")
	}
	if err := validateDirectory(planner.options.CodexHome); err != nil {
		return Plan{}, err
	}
	version, err := planner.versions.Version(ctx, planner.options.AllowUntested)
	if err != nil {
		return Plan{}, err
	}
	catalogResponse, err := planner.catalog.CodexCatalog(ctx)
	if err != nil || !validCatalogResponse(catalogResponse) {
		return Plan{}, errors.Join(ErrUnavailable, errors.New("Codex catalog unavailable"))
	}
	profilePath := filepath.Join(planner.options.CodexHome, "ant-router.config.toml")
	catalogPath := filepath.Join(planner.options.StateRoot, "codex", "ant-router", "models.json")
	profile, err := codexprofile.Render(codexprofile.Options{BaseURL: planner.options.BaseURL, CatalogPath: catalogPath, RouterctlPath: planner.options.RouterctlPath, Instance: planner.options.Instance, StateRoot: planner.options.StateRoot, RuntimeRoot: planner.options.RuntimeRoot})
	if err != nil {
		return Plan{}, err
	}
	profileObservation, profileBytes, err := observeFile(ctx, profilePath, codexownership.MaxProfileBytes)
	if err != nil {
		return Plan{}, err
	}
	catalogObservation, catalogBytes, err := observeFile(ctx, catalogPath, codexownership.MaxCatalogBytes)
	if err != nil {
		return Plan{}, err
	}
	ownershipGeneration := ""
	manifest, generation, ownershipErr := planner.ownership.Current(ctx)
	if ownershipErr == nil {
		if manifest.ProfilePath != profilePath || manifest.CatalogPath != catalogPath {
			return Plan{}, errors.New("Codex ownership path conflict")
		}
		ownershipGeneration = generation.ID
		if profileObservation.Exists && !bytesEqual(profileBytes, generation.Profile) {
			return Plan{}, errors.New("Codex profile ownership drift")
		}
		if catalogObservation.Exists && !bytesEqual(catalogBytes, generation.Catalog) {
			return Plan{}, errors.New("Codex catalog ownership drift")
		}
	} else if !errors.Is(ownershipErr, os.ErrNotExist) {
		return Plan{}, errors.New("Codex ownership unavailable")
	} else if profileObservation.Exists || catalogObservation.Exists {
		return Plan{}, errors.Join(ErrConflict, errors.New("Codex integration ownership conflict"))
	}
	return NewPlan(PlanInput{Action: ActionEnable, ProfilePath: profilePath, CatalogPath: catalogPath, RouterctlPath: planner.options.RouterctlPath, CodexVersion: version.String(), ConfigGeneration: catalogResponse.Generation, CurrentProfile: profileObservation, CurrentCatalog: catalogObservation, OwnershipGeneration: ownershipGeneration, DesiredProfile: profile, DesiredCatalog: []byte(catalogResponse.Catalog)})
}

func (planner *Planner) PlanDisable(ctx context.Context) (Plan, error) {
	profilePath, catalogPath, profileObservation, catalogObservation, generation, owned, err := planner.observeOwned(ctx)
	if err != nil {
		return Plan{}, err
	}
	version, routerctlPath, ownershipGeneration := "unknown", planner.options.RouterctlPath, ""
	if owned {
		version, routerctlPath, ownershipGeneration = generation.CodexVersion, generation.RouterctlPath, generation.ID
	}
	return NewPlan(PlanInput{Action: ActionDisable, ProfilePath: profilePath, CatalogPath: catalogPath, RouterctlPath: routerctlPath, CodexVersion: version, CurrentProfile: profileObservation, CurrentCatalog: catalogObservation, OwnershipGeneration: ownershipGeneration})
}

func (planner *Planner) PlanRestore(ctx context.Context, generationID string) (Plan, error) {
	profilePath, catalogPath, profileObservation, catalogObservation, current, owned, err := planner.observeOwned(ctx)
	if err != nil {
		return Plan{}, err
	}
	if !owned {
		return Plan{}, errors.New("Codex integration is not owned")
	}
	target, err := planner.ownership.Generation(ctx, generationID)
	if err != nil || target.ProfilePath != profilePath || target.CatalogPath != catalogPath {
		return Plan{}, errors.New("Codex restore generation unavailable")
	}
	return NewPlan(PlanInput{Action: ActionRestore, ProfilePath: profilePath, CatalogPath: catalogPath, RouterctlPath: target.RouterctlPath, CodexVersion: target.CodexVersion, CurrentProfile: profileObservation, CurrentCatalog: catalogObservation, OwnershipGeneration: current.ID, DesiredProfile: target.Profile, DesiredCatalog: target.Catalog})
}

func (planner *Planner) observeOwned(ctx context.Context) (string, string, FileObservation, FileObservation, codexownership.Generation, bool, error) {
	if planner == nil || ctx == nil {
		return "", "", FileObservation{}, FileObservation{}, codexownership.Generation{}, false, errors.New("invalid Codex plan request")
	}
	if err := validateDirectory(planner.options.CodexHome); err != nil {
		return "", "", FileObservation{}, FileObservation{}, codexownership.Generation{}, false, err
	}
	profilePath := filepath.Join(planner.options.CodexHome, "ant-router.config.toml")
	catalogPath := filepath.Join(planner.options.StateRoot, "codex", "ant-router", "models.json")
	profileObservation, profileBytes, err := observeFile(ctx, profilePath, codexownership.MaxProfileBytes)
	if err != nil {
		return "", "", FileObservation{}, FileObservation{}, codexownership.Generation{}, false, err
	}
	catalogObservation, catalogBytes, err := observeFile(ctx, catalogPath, codexownership.MaxCatalogBytes)
	if err != nil {
		return "", "", FileObservation{}, FileObservation{}, codexownership.Generation{}, false, err
	}
	manifest, generation, err := planner.ownership.Current(ctx)
	if errors.Is(err, os.ErrNotExist) {
		if profileObservation.Exists || catalogObservation.Exists {
			return "", "", FileObservation{}, FileObservation{}, codexownership.Generation{}, false, errors.New("Codex integration ownership conflict")
		}
		return profilePath, catalogPath, profileObservation, catalogObservation, codexownership.Generation{}, false, nil
	}
	if err != nil || manifest.ProfilePath != profilePath || manifest.CatalogPath != catalogPath {
		return "", "", FileObservation{}, FileObservation{}, codexownership.Generation{}, false, errors.New("Codex ownership unavailable")
	}
	if profileObservation.Exists && !bytesEqual(profileBytes, generation.Profile) {
		return "", "", FileObservation{}, FileObservation{}, codexownership.Generation{}, false, errors.New("Codex profile ownership drift")
	}
	if catalogObservation.Exists && !bytesEqual(catalogBytes, generation.Catalog) {
		return "", "", FileObservation{}, FileObservation{}, codexownership.Generation{}, false, errors.New("Codex catalog ownership drift")
	}
	return profilePath, catalogPath, profileObservation, catalogObservation, generation, true, nil
}

func validCatalogResponse(response control.CodexCatalogResponse) bool {
	data := []byte(response.Catalog)
	if response.APIVersion != control.APIVersion || response.Generation == 0 || len(data) == 0 || len(data) > control.MaxCodexCatalogBytes || !json.Valid(data) {
		return false
	}
	digest := sha256.Sum256(data)
	return response.SHA256 == hex.EncodeToString(digest[:])
}

func validateDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || !validateDirectorySecurity(path, info) {
		return errors.New("invalid Codex home")
	}
	return nil
}

func observeFile(ctx context.Context, path string, limit int) (FileObservation, []byte, error) {
	if err := ctx.Err(); err != nil {
		return FileObservation{}, nil, err
	}
	before, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return FileObservation{}, nil, nil
	}
	if err != nil || !before.Mode().IsRegular() || before.Mode()&os.ModeSymlink != 0 || before.Size() < 0 || before.Size() > int64(limit) {
		return FileObservation{}, nil, errors.New("invalid Codex integration file")
	}
	file, err := os.Open(path)
	if err != nil {
		return FileObservation{}, nil, errors.New("Codex integration file unavailable")
	}
	data, readErr := io.ReadAll(io.LimitReader(file, int64(limit)+1))
	after, statErr := file.Stat()
	closeErr := file.Close()
	if readErr != nil || statErr != nil || closeErr != nil || !os.SameFile(before, after) || len(data) > limit {
		return FileObservation{}, nil, errors.New("Codex integration file unavailable")
	}
	if err := ctx.Err(); err != nil {
		return FileObservation{}, nil, err
	}
	digest := sha256.Sum256(data)
	return FileObservation{Exists: true, SHA256: hex.EncodeToString(digest[:])}, data, nil
}

func nilInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

func bytesEqual(left, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
