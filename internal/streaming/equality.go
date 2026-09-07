package streaming

import (
	"bytes"

	"github.com/abyssbugg/ant-router/internal/canonical"
)

func contentEqual(left, right canonical.ContentBlock) bool {
	if left.Type() != right.Type() {
		return false
	}
	switch left.Type() {
	case canonical.ContentType("text"):
		first, ok1 := left.Text()
		second, ok2 := right.Text()
		return ok1 && ok2 && first.Text() == second.Text()
	case canonical.ContentType("json"):
		first, ok1 := left.JSON()
		second, ok2 := right.JSON()
		return ok1 && ok2 && first.Value().Equal(second.Value())
	case canonical.ContentType("image"):
		first, ok1 := left.Image()
		second, ok2 := right.Image()
		if !ok1 || !ok2 || first.SourceType() != second.SourceType() || first.MIME() != second.MIME() || first.Detail() != second.Detail() || first.Width() != second.Width() || first.Height() != second.Height() {
			return false
		}
		if firstURL, firstOK := first.URL(); firstOK {
			secondURL, secondOK := second.URL()
			return secondOK && firstURL == secondURL
		}
		firstBytes, _ := first.Bytes()
		secondBytes, secondOK := second.Bytes()
		return secondOK && bytes.Equal(firstBytes, secondBytes)
	default:
		return false
	}
}

func itemEqual(left, right canonical.Item) bool {
	if left.Type() != right.Type() || left.ID() != right.ID() {
		return false
	}
	switch left.Type() {
	case canonical.ItemType("message"):
		first, ok1 := left.Message()
		second, ok2 := right.Message()
		if !ok1 || !ok2 || first.Role() != second.Role() {
			return false
		}
		return contentSlicesEqual(first.Content(), second.Content())
	case canonical.ItemType("tool_call"):
		first, ok1 := left.ToolCall()
		second, ok2 := right.ToolCall()
		return ok1 && ok2 && first.CallID() == second.CallID() && first.Namespace() == second.Namespace() && first.Name() == second.Name() && first.Arguments().Equal(second.Arguments())
	case canonical.ItemType("reasoning_summary"):
		first, ok1 := left.ReasoningSummary()
		second, ok2 := right.ReasoningSummary()
		if !ok1 || !ok2 {
			return false
		}
		a, b := first.Summaries(), second.Summaries()
		if len(a) != len(b) {
			return false
		}
		for i := range a {
			if a[i].Text() != b[i].Text() {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func contentSlicesEqual(left, right []canonical.ContentBlock) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if !contentEqual(left[index], right[index]) {
			return false
		}
	}
	return true
}

func contentSize(content canonical.ContentBlock) int {
	switch content.Type() {
	case canonical.ContentType("text"):
		value, _ := content.Text()
		return len(value.Text())
	case canonical.ContentType("json"):
		value, _ := content.JSON()
		return len(value.Value().Bytes())
	case canonical.ContentType("image"):
		value, _ := content.Image()
		if data, ok := value.Bytes(); ok {
			return len(data)
		}
		raw, _ := value.URL()
		return len(raw)
	default:
		return 0
	}
}
