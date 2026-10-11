package configservice

import (
	"bytes"
	"context"
	"errors"
	"sync"

	"github.com/abyssbugg/ant-router/internal/config"
	"github.com/abyssbugg/ant-router/internal/configstore"
)

type Service struct {
	mu           sync.RWMutex
	manager      *config.Manager
	store        configstore.Store
	state        State
	diagnostic   error
	initialized  bool
	onActivation func(config.Snapshot)
}

func New(manager *config.Manager, store configstore.Store) (*Service, error) {
	if manager == nil || store == nil {
		return nil, errors.New("configuration service dependencies are required")
	}
	return &Service{manager: manager, store: store, state: StateAwaitingConfiguration}, nil
}

func (s *Service) Initialize(ctx context.Context) (State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.initialized {
		return s.state, &Error{Code: CodeConfigurationUnavailable, Err: errors.New("service already initialized")}
	}
	s.initialized = true
	_, err := s.manager.LoadManagedStartup(ctx)
	if err == nil {
		s.state = StateRunning
		s.diagnostic = nil
		return s.state, nil
	}
	if code, ok := config.CodeOf(err); ok && code == config.CodeNoConfiguration {
		s.state = StateAwaitingConfiguration
		s.diagnostic = nil
		return s.state, nil
	}
	s.state = StateDegraded
	s.diagnostic = err
	return s.state, err
}

func (s *Service) Validate(ctx context.Context, tomlBytes []byte) (ValidationResult, error) {
	if err := ctx.Err(); err != nil {
		return ValidationResult{}, &Error{Code: CodeCancelled, Err: err}
	}
	if len(tomlBytes) > config.MaxSourceBytes {
		return ValidationResult{}, &Error{Code: CodeConfigurationTooLarge}
	}
	configuration, err := config.Decode(bytes.NewReader(tomlBytes))
	if err != nil {
		return ValidationResult{}, &Error{Code: CodeConfigurationInvalid, Err: err}
	}
	generation := uint64(0)
	if current, ok := s.manager.Current(); ok {
		generation = current.Generation()
	}
	candidate, err := config.NewCandidate(configuration, "", generation)
	if err != nil {
		return ValidationResult{}, &Error{Code: CodeConfigurationInvalid, Err: err}
	}
	return ValidationResult{Revision: candidate.RevisionID(), BaseGeneration: generation, CanonicalSize: len(candidate.Canonical())}, nil
}

// SetActivationCallback registers a callback invoked after every successful
// configuration activation. The callback runs outside the service lock and
// must not call service methods that take it.
func (s *Service) SetActivationCallback(callback func(config.Snapshot)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onActivation = callback
}

func (s *Service) notifyActivation(snapshot config.Snapshot) {
	s.mu.RLock()
	callback := s.onActivation
	s.mu.RUnlock()
	if callback != nil {
		callback(snapshot)
	}
}

func (s *Service) Apply(ctx context.Context, request ApplyRequest) (CurrentResult, error) {
	snapshot, err := s.apply(ctx, request)
	if err != nil {
		return CurrentResult{}, err
	}
	s.notifyActivation(snapshot)
	return resultFromSnapshot(StateRunning, snapshot), nil
}

func (s *Service) apply(ctx context.Context, request ApplyRequest) (config.Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return config.Snapshot{}, &Error{Code: CodeCancelled, Err: err}
	}
	if len(request.TOML) > config.MaxSourceBytes {
		return config.Snapshot{}, &Error{Code: CodeConfigurationTooLarge}
	}
	configuration, err := config.Decode(bytes.NewReader(request.TOML))
	if err != nil {
		return config.Snapshot{}, &Error{Code: CodeConfigurationInvalid, Err: err}
	}
	candidate, err := config.NewCandidate(configuration, "", request.ExpectedGeneration)
	if err != nil {
		return config.Snapshot{}, &Error{Code: CodeConfigurationInvalid, Err: err}
	}
	if !request.ExpectedRevision.Valid() || candidate.RevisionID() != request.ExpectedRevision {
		return config.Snapshot{}, &Error{Code: CodeRevisionMismatch}
	}
	currentGeneration := uint64(0)
	if current, ok := s.manager.Current(); ok {
		currentGeneration = current.Generation()
	}
	if currentGeneration != request.ExpectedGeneration {
		return config.Snapshot{}, &Error{Code: CodeGenerationConflict}
	}
	snapshot, err := s.manager.Activate(ctx, candidate)
	if err != nil {
		if code, ok := configstore.CodeOf(err); ok && code == configstore.CodeRevisionCorrupt {
			if repairErr := s.store.RepairRevision(ctx, candidate.RevisionID(), candidate.Canonical()); repairErr != nil {
				if repairCode, _ := configstore.CodeOf(repairErr); repairCode != configstore.CodeRepairUnnecessary {
					return config.Snapshot{}, mapError(repairErr)
				}
			}
			snapshot, err = s.manager.Activate(ctx, candidate)
		}
	}
	if err != nil {
		return config.Snapshot{}, mapError(err)
	}
	s.state = StateRunning
	s.diagnostic = nil
	return snapshot, nil
}

func (s *Service) Current() CurrentResult {
	s.mu.RLock()
	defer s.mu.RUnlock()
	diagnostic := ""
	if s.diagnostic != nil {
		diagnostic = s.diagnostic.Error()
	}
	if snapshot, ok := s.manager.Current(); ok {
		result := resultFromSnapshot(s.state, snapshot)
		result.Diagnostic = diagnostic
		return result
	}
	return CurrentResult{State: s.state, Diagnostic: diagnostic}
}

func (s *Service) Rollback(ctx context.Context) (CurrentResult, error) {
	s.mu.Lock()
	if s.state != StateRunning {
		s.mu.Unlock()
		return CurrentResult{}, &Error{Code: CodeNoPreviousRevision}
	}
	snapshot, err := s.manager.Rollback(ctx)
	if err != nil {
		s.mu.Unlock()
		return CurrentResult{}, mapError(err)
	}
	s.mu.Unlock()
	s.notifyActivation(snapshot)
	return resultFromSnapshot(StateRunning, snapshot), nil
}

func resultFromSnapshot(state State, snapshot config.Snapshot) CurrentResult {
	return CurrentResult{State: state, Revision: snapshot.RevisionID(), Generation: snapshot.Generation(), Status: snapshot.Status(), ActivatedAt: snapshot.ActivatedAt()}
}

func mapError(err error) error {
	if code, ok := config.CodeOf(err); ok {
		switch code {
		case config.CodeCancelled:
			return &Error{Code: CodeCancelled, Err: err}
		case config.CodeStaleCandidate:
			return &Error{Code: CodeGenerationConflict, Err: err}
		case config.CodeNoPreviousRevision:
			return &Error{Code: CodeNoPreviousRevision, Err: err}
		case config.CodeValidationFailed, config.CodeUnsupportedSchema:
			return &Error{Code: CodeConfigurationInvalid, Err: err}
		}
	}
	if code, ok := configstore.CodeOf(err); ok {
		if code == configstore.CodeCancelled {
			return &Error{Code: CodeCancelled, Err: err}
		}
		return &Error{Code: CodeConfigurationUnavailable, Err: err}
	}
	return &Error{Code: CodeConfigurationUnavailable, Err: err}
}
