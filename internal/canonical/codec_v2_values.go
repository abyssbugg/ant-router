package canonical

func encodeCustomToolFormat(value CustomToolFormat) *customToolFormatDTO {
	return &customToolFormatDTO{Type: string(value.Kind()), Syntax: string(value.Syntax()), Definition: value.Definition()}
}

func decodeCustomToolFormat(dto *customToolFormatDTO) (CustomToolFormat, error) {
	if dto == nil {
		return CustomToolFormat{}, codecError(CodeInvalidInvariant, "tool.custom_format")
	}
	switch CustomToolFormatKind(dto.Type) {
	case CustomToolFormatKindText:
		if dto.Syntax != "" || dto.Definition != "" {
			return CustomToolFormat{}, codecError(CodeInvalidInvariant, "tool.custom_format")
		}
		return NewCustomTextFormat(), nil
	case CustomToolFormatKindGrammar:
		return NewCustomGrammarFormat(CustomGrammarSyntax(dto.Syntax), dto.Definition)
	default:
		return CustomToolFormat{}, codecError(CodeInvalidVariant, "tool.custom_format.type")
	}
}

func encodeHostedSearchAction(value HostedSearchAction) hostedSearchActionDTO {
	return hostedSearchActionDTO{Type: string(value.Kind()), Queries: value.Queries(), Sources: value.Sources(), URL: value.URL(), Pattern: value.Pattern()}
}

func decodeHostedSearchAction(dto hostedSearchActionDTO) (HostedSearchAction, error) {
	switch HostedSearchActionKind(dto.Type) {
	case HostedSearchActionKindSearch:
		if dto.URL != "" || dto.Pattern != "" {
			return HostedSearchAction{}, codecError(CodeInvalidInvariant, "hosted_search.action")
		}
		return NewHostedSearchActionSearch(dto.Queries, dto.Sources)
	case HostedSearchActionKindOpenPage:
		if len(dto.Queries) != 0 || len(dto.Sources) != 0 || dto.Pattern != "" {
			return HostedSearchAction{}, codecError(CodeInvalidInvariant, "hosted_search.action")
		}
		return NewHostedSearchActionOpenPage(dto.URL)
	case HostedSearchActionKindFindInPage:
		if len(dto.Queries) != 0 || len(dto.Sources) != 0 {
			return HostedSearchAction{}, codecError(CodeInvalidInvariant, "hosted_search.action")
		}
		return NewHostedSearchActionFindInPage(dto.URL, dto.Pattern)
	default:
		return HostedSearchAction{}, codecError(CodeInvalidVariant, "hosted_search.action.type")
	}
}

func encodeURLCitation(value URLCitation) urlCitationDTO {
	return urlCitationDTO{StartIndex: value.StartIndex(), EndIndex: value.EndIndex(), Title: value.Title(), URL: value.URL()}
}

func decodeURLCitation(dto urlCitationDTO) (URLCitation, error) {
	return NewURLCitation(dto.StartIndex, dto.EndIndex, dto.Title, dto.URL)
}
