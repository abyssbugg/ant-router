package canonical

import (
	"unicode"
	"unicode/utf8"

	"github.com/abyssbugg/ant-router/internal/canonicaljson"
)

const (
	MaxHostedSearchOptionsBytes     = 64 << 10
	MaxHostedSearchContentTypes     = 16
	MaxHostedSearchContentTypeBytes = 128
)

type HostedSearchContextSize string

const (
	HostedSearchContextLow    HostedSearchContextSize = "low"
	HostedSearchContextMedium HostedSearchContextSize = "medium"
	HostedSearchContextHigh   HostedSearchContextSize = "high"
)

func (s HostedSearchContextSize) Valid() bool {
	return s == "" || s == HostedSearchContextLow || s == HostedSearchContextMedium || s == HostedSearchContextHigh
}

type HostedWebSearchOptionsParams struct {
	ExternalWebAccess  *bool
	IndexedWebAccess   *bool
	Filters            canonicaljson.Value
	UserLocation       canonicaljson.Value
	SearchContextSize  HostedSearchContextSize
	SearchContentTypes []string
}

type HostedWebSearchOptions struct {
	externalSet  bool
	external     bool
	indexedSet   bool
	indexed      bool
	filters      canonicaljson.Value
	userLocation canonicaljson.Value
	contextSize  HostedSearchContextSize
	contentTypes []string
}

func NewHostedWebSearchOptions(params HostedWebSearchOptionsParams) (HostedWebSearchOptions, error) {
	value := HostedWebSearchOptions{filters: params.Filters, userLocation: params.UserLocation, contextSize: params.SearchContextSize, contentTypes: append([]string(nil), params.SearchContentTypes...)}
	if params.ExternalWebAccess != nil {
		value.externalSet, value.external = true, *params.ExternalWebAccess
	}
	if params.IndexedWebAccess != nil {
		value.indexedSet, value.indexed = true, *params.IndexedWebAccess
	}
	if !value.Valid() {
		return HostedWebSearchOptions{}, itemError(CodeInvalidInvariant, "tool.web_search")
	}
	return value, nil
}

func (o HostedWebSearchOptions) ExternalWebAccess() (bool, bool)            { return o.external, o.externalSet }
func (o HostedWebSearchOptions) IndexedWebAccess() (bool, bool)             { return o.indexed, o.indexedSet }
func (o HostedWebSearchOptions) Filters() canonicaljson.Value               { return o.filters }
func (o HostedWebSearchOptions) UserLocation() canonicaljson.Value          { return o.userLocation }
func (o HostedWebSearchOptions) SearchContextSize() HostedSearchContextSize { return o.contextSize }
func (o HostedWebSearchOptions) SearchContentTypes() []string {
	return append([]string(nil), o.contentTypes...)
}

func (o HostedWebSearchOptions) Valid() bool {
	if !o.contextSize.Valid() || len(o.contentTypes) > MaxHostedSearchContentTypes {
		return false
	}
	if o.filters.Kind() != canonicaljson.KindInvalid && (!o.filters.IsObject() || len(o.filters.Bytes()) > MaxHostedSearchOptionsBytes) {
		return false
	}
	if o.userLocation.Kind() != canonicaljson.KindInvalid && (!o.userLocation.IsObject() || len(o.userLocation.Bytes()) > MaxHostedSearchOptionsBytes) {
		return false
	}
	seen := make(map[string]struct{}, len(o.contentTypes))
	for _, value := range o.contentTypes {
		if value == "" || !utf8.ValidString(value) || len(value) > MaxHostedSearchContentTypeBytes {
			return false
		}
		for _, r := range value {
			if unicode.IsControl(r) {
				return false
			}
		}
		if _, ok := seen[value]; ok {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}
