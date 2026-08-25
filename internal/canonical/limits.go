package canonical

const (
	// MaxRequestBytes limits a fully encoded canonical request.
	MaxRequestBytes = 32 << 20
	// MaxInputItems limits ordered request input items.
	MaxInputItems = 1024
	// MaxContentBlocks limits content blocks within one item.
	MaxContentBlocks = 256
	// MaxTextBytes limits one UTF-8 text block.
	MaxTextBytes = 1 << 20
	// MaxJSONBytes limits one canonical JSON value.
	MaxJSONBytes = 1 << 20
	// MaxJSONDepth limits nested canonical JSON containers.
	MaxJSONDepth = 64
	// MaxTools limits tool definitions in one request.
	MaxTools = 128
	// MaxToolSchemaBytes limits one tool input schema.
	MaxToolSchemaBytes = 256 << 10
	// MaxInlineImageBytes limits one inline image payload.
	MaxInlineImageBytes = 20 << 20
	// MaxImageURLBytes limits one remote image URL.
	MaxImageURLBytes = 2048
	// MaxImageDimension limits image width and height.
	MaxImageDimension = 32768
	// MaxContinuationBytes limits opaque provider continuation state.
	MaxContinuationBytes = 256 << 10
	// MaxOrdinaryEventBytes limits events that do not contain completed images.
	MaxOrdinaryEventBytes = 2 << 20
)
