package canonical

import "unicode/utf8"

func hostedSearchActionErrorCode(value HostedSearchAction) ErrorCode {
	if len(value.queries) > MaxHostedSearchQueries || len(value.sources) > MaxHostedSearchSources {
		return CodeLimitExceeded
	}
	for _, query := range value.queries {
		if !utf8.ValidString(query) {
			return CodeInvalidUTF8
		}
		if len(query) > MaxHostedSearchQueryBytes {
			return CodeLimitExceeded
		}
	}
	for _, source := range value.sources {
		if !utf8.ValidString(source) {
			return CodeInvalidUTF8
		}
		if len(source) > MaxHostedSearchURLBytes {
			return CodeLimitExceeded
		}
	}
	if !utf8.ValidString(value.url) || !utf8.ValidString(value.pattern) {
		return CodeInvalidUTF8
	}
	if len(value.url) > MaxHostedSearchURLBytes || len(value.pattern) > MaxHostedSearchPatternBytes {
		return CodeLimitExceeded
	}
	return CodeInvalidInvariant
}

// HostedSearchStatus identifies hosted-search execution state.
type HostedSearchStatus string

const (
	HostedSearchStatusInProgress HostedSearchStatus = "in_progress"
	HostedSearchStatusSearching  HostedSearchStatus = "searching"
	HostedSearchStatusCompleted  HostedSearchStatus = "completed"
	HostedSearchStatusFailed     HostedSearchStatus = "failed"
)

func (s HostedSearchStatus) Valid() bool {
	return s == HostedSearchStatusInProgress || s == HostedSearchStatusSearching || s == HostedSearchStatusCompleted || s == HostedSearchStatusFailed
}
func (s HostedSearchStatus) Terminal() bool {
	return s == HostedSearchStatusCompleted || s == HostedSearchStatusFailed
}

// HostedSearchActionKind identifies one provider-hosted search action.
type HostedSearchActionKind string

const (
	HostedSearchActionKindSearch     HostedSearchActionKind = "search"
	HostedSearchActionKindOpenPage   HostedSearchActionKind = "open_page"
	HostedSearchActionKindFindInPage HostedSearchActionKind = "find_in_page"
)

func (k HostedSearchActionKind) Valid() bool {
	return k == HostedSearchActionKindSearch || k == HostedSearchActionKindOpenPage || k == HostedSearchActionKindFindInPage
}

// HostedSearchAction is an immutable provider-neutral search action.
type HostedSearchAction struct {
	kind    HostedSearchActionKind
	queries []string
	sources []string
	url     string
	pattern string
}

func NewHostedSearchActionSearch(queries, sources []string) (HostedSearchAction, error) {
	value := HostedSearchAction{kind: HostedSearchActionKindSearch, queries: append([]string(nil), queries...), sources: append([]string(nil), sources...)}
	if !value.Valid() {
		return HostedSearchAction{}, itemError(hostedSearchActionErrorCode(value), "hosted_search.action")
	}
	return value, nil
}
func NewHostedSearchActionOpenPage(reference string) (HostedSearchAction, error) {
	value := HostedSearchAction{kind: HostedSearchActionKindOpenPage, url: reference}
	if !value.Valid() {
		return HostedSearchAction{}, itemError(hostedSearchActionErrorCode(value), "hosted_search.action")
	}
	return value, nil
}
func NewHostedSearchActionFindInPage(reference, pattern string) (HostedSearchAction, error) {
	value := HostedSearchAction{kind: HostedSearchActionKindFindInPage, url: reference, pattern: pattern}
	if !value.Valid() {
		return HostedSearchAction{}, itemError(hostedSearchActionErrorCode(value), "hosted_search.action")
	}
	return value, nil
}

func (a HostedSearchAction) Kind() HostedSearchActionKind { return a.kind }
func (a HostedSearchAction) Queries() []string            { return append([]string(nil), a.queries...) }
func (a HostedSearchAction) Sources() []string            { return append([]string(nil), a.sources...) }
func (a HostedSearchAction) URL() string                  { return a.url }
func (a HostedSearchAction) Pattern() string              { return a.pattern }
func (a HostedSearchAction) Valid() bool {
	switch a.kind {
	case HostedSearchActionKindSearch:
		if len(a.queries) == 0 || len(a.queries) > MaxHostedSearchQueries || len(a.sources) > MaxHostedSearchSources || a.url != "" || a.pattern != "" {
			return false
		}
		for _, query := range a.queries {
			if !validBoundedMetadata(query, MaxHostedSearchQueryBytes, false) {
				return false
			}
		}
		for _, source := range a.sources {
			if !validHTTPSReference(source) {
				return false
			}
		}
		return true
	case HostedSearchActionKindOpenPage:
		return len(a.queries) == 0 && len(a.sources) == 0 && a.pattern == "" && validHTTPSReference(a.url)
	case HostedSearchActionKindFindInPage:
		return len(a.queries) == 0 && len(a.sources) == 0 && validHTTPSReference(a.url) && validBoundedMetadata(a.pattern, MaxHostedSearchPatternBytes, false)
	default:
		return false
	}
}

// HostedSearchCall is an immutable terminal hosted-search output item.
type HostedSearchCall struct {
	id     ItemID
	status HostedSearchStatus
	action HostedSearchAction
}

func NewHostedSearchCall(id ItemID, status HostedSearchStatus, action HostedSearchAction) (HostedSearchCall, error) {
	value := HostedSearchCall{id: id, status: status, action: action}
	if !value.Valid() {
		code := CodeInvalidInvariant
		if !id.Valid() {
			code = CodeInvalidID
		}
		return HostedSearchCall{}, itemError(code, "hosted_search_call")
	}
	return value, nil
}
func (c HostedSearchCall) ID() ItemID                 { return c.id }
func (c HostedSearchCall) Status() HostedSearchStatus { return c.status }
func (c HostedSearchCall) Action() HostedSearchAction { return c.action }
func (c HostedSearchCall) Valid() bool {
	return c.id.Valid() && c.status.Terminal() && c.action.Valid()
}
