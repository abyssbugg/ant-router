package streaming

import "github.com/abyssbugg/ant-router/internal/canonical"

func citationsEqual(left, right canonical.URLCitation) bool {
	return left.StartIndex() == right.StartIndex() && left.EndIndex() == right.EndIndex() && left.Title() == right.Title() && left.URL() == right.URL()
}

func citationSlicesEqual(left, right []canonical.URLCitation) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if !citationsEqual(left[index], right[index]) {
			return false
		}
	}
	return true
}

func citationSize(value canonical.URLCitation) int {
	return len(value.Title()) + len(value.URL())
}

func citationSliceSize(values []canonical.URLCitation) int {
	total := 0
	for _, value := range values {
		total += citationSize(value)
	}
	return total
}

func textContentEqual(left, right canonical.TextContent) bool {
	return left.Text() == right.Text() && citationSlicesEqual(left.Citations(), right.Citations())
}

func hostedSearchActionEqual(left, right canonical.HostedSearchAction) bool {
	if left.Kind() != right.Kind() || left.URL() != right.URL() || left.Pattern() != right.Pattern() {
		return false
	}
	leftQueries, rightQueries := left.Queries(), right.Queries()
	if len(leftQueries) != len(rightQueries) {
		return false
	}
	for index := range leftQueries {
		if leftQueries[index] != rightQueries[index] {
			return false
		}
	}
	leftSources, rightSources := left.Sources(), right.Sources()
	if len(leftSources) != len(rightSources) {
		return false
	}
	for index := range leftSources {
		if leftSources[index] != rightSources[index] {
			return false
		}
	}
	return true
}
