package canonical

// Message is an immutable canonical role/content item.
type Message struct {
	id      ItemID
	role    Role
	content []ContentBlock
}

// NewMessage validates and copies a canonical message.
func NewMessage(id ItemID, role Role, content []ContentBlock) (Message, error) {
	if !id.Valid() {
		return Message{}, itemError(CodeInvalidID, "message.id")
	}
	if !role.Valid() {
		return Message{}, itemError(CodeInvalidRole, "message.role")
	}
	if err := validateContentBlocks(content, "message.content"); err != nil {
		return Message{}, err
	}
	return Message{id: id, role: role, content: append([]ContentBlock(nil), content...)}, nil
}

// ID returns the message item ID.
func (m Message) ID() ItemID { return m.id }

// Role returns the message role.
func (m Message) Role() Role { return m.role }

// Content returns a defensive content-block copy.
func (m Message) Content() []ContentBlock { return append([]ContentBlock(nil), m.content...) }

// Valid reports whether the message satisfies local invariants.
func (m Message) Valid() bool {
	return m.id.Valid() && m.role.Valid() && validateContentBlocks(m.content, "message.content") == nil
}

func validateContentBlocks(content []ContentBlock, path string) error {
	if len(content) == 0 {
		return itemError(CodeInvalidInvariant, path)
	}
	if len(content) > MaxContentBlocks {
		return itemError(CodeLimitExceeded, path)
	}
	for _, block := range content {
		if !block.Valid() {
			return itemError(CodeInvalidInvariant, path)
		}
	}
	return nil
}
