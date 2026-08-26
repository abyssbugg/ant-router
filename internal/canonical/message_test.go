package canonical

import "testing"

func TestMessageDefensiveCopy(t *testing.T) {
	blocks := []ContentBlock{testTextBlock(t, "hello")}
	message, err := NewMessage(testItemID(t, 1), Role("user"), blocks)
	if err != nil || !message.Valid() || message.Role() != Role("user") {
		t.Fatalf("NewMessage() = %#v, %v", message, err)
	}
	blocks[0] = ContentBlock{}
	first := message.Content()
	first[0] = ContentBlock{}
	second := message.Content()
	if len(second) != 1 || !second[0].Valid() || message.ID() != testItemID(t, 1) {
		t.Error("message content was mutated")
	}
}

func TestMessageRejectsInvalid(t *testing.T) {
	valid := testTextBlock(t, "hello")
	tooMany := make([]ContentBlock, MaxContentBlocks+1)
	for index := range tooMany {
		tooMany[index] = valid
	}
	tests := []struct {
		name    string
		id      ItemID
		role    Role
		content []ContentBlock
		code    ErrorCode
	}{
		{"id", "", Role("user"), []ContentBlock{valid}, CodeInvalidID},
		{"role", testItemID(t, 1), Role("tool"), []ContentBlock{valid}, CodeInvalidRole},
		{"empty", testItemID(t, 1), Role("user"), nil, CodeInvalidInvariant},
		{"too many", testItemID(t, 1), Role("user"), tooMany, CodeLimitExceeded},
		{"invalid block", testItemID(t, 1), Role("user"), []ContentBlock{{}}, CodeInvalidInvariant},
	}
	for _, test := range tests {
		if _, err := NewMessage(test.id, test.role, test.content); codeOfCanonical(err) != test.code {
			t.Errorf("%s code = %q, want %q", test.name, codeOfCanonical(err), test.code)
		}
	}
}
