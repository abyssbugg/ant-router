package execution

import (
	"context"
	"crypto/rand"
	"strings"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/streaming"
)

// visionBridgePrompt instructs the vision model how to describe the attached
// images. The output is injected verbatim; the numbering convention is a
// client-facing courtesy, never parsed by the router.
const visionBridgePrompt = "Describe each attached image precisely and in order. Begin each description with 'Image N:' using the 1-based order of the image."

// VisionBridge serves image-bearing requests on models that cannot see. When
// the target model lacks the image_input capability, the bridge first asks a
// configured vision-capable model to describe the attached images, then
// forwards the request with the description text injected in place of the
// images. Everything else passes through untouched.
//
// The describe sub-call always targets the inner executor directly, so a
// bridge model never re-enters the bridge. Failures of the describe call are
// surfaced to the client instead of silently degrading to a text-only turn.
type VisionBridge struct {
	inner       Executor
	bridgeModel string
	ids         bridgeIDs
}

// bridgeIDs generates canonical identifiers for internal bridge requests.
type bridgeIDs interface {
	ItemID() (canonical.ItemID, error)
}

type randomBridgeIDs struct{}

func (randomBridgeIDs) ItemID() (canonical.ItemID, error) {
	return canonical.GenerateItemID(rand.Reader)
}

// NewVisionBridge wraps inner with the vision bridge bound to bridgeModel's
// public alias.
func NewVisionBridge(inner Executor, bridgeModel string) *VisionBridge {
	return &VisionBridge{inner: inner, bridgeModel: bridgeModel, ids: randomBridgeIDs{}}
}

// Execute implements Executor.
func (b *VisionBridge) Execute(ctx context.Context, request canonical.Request) (streaming.EventSource, error) {
	if b == nil || b.inner == nil || ctx == nil {
		return nil, NewError(CodeInternal, "vision_bridge", 0, nil)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	images := collectImageBlocks(request)
	if len(images) == 0 {
		return b.inner.Execute(ctx, request)
	}
	if b.targetSeesImages(ctx, request.Model()) {
		return b.inner.Execute(ctx, request)
	}
	if resolver, ok := b.inner.(AffinityResolver); ok {
		// The registry will reject an unresolvable target; fail there before
		// shipping the user's images to the vision provider.
		if _, _, resolveErr := resolver.ResolveModel(request.Model()); resolveErr != nil {
			return b.inner.Execute(ctx, request)
		}
	}
	described, err := b.describe(ctx, request, images)
	if err != nil {
		return nil, err
	}
	enriched, err := rebuildWithoutImages(request, described)
	if err != nil {
		return nil, NewError(CodeInternal, "vision_bridge.request", 0, err)
	}
	return b.inner.Execute(ctx, enriched)
}

type imageLocation struct {
	itemIndex  int
	blockIndex int
}

// collectImageBlocks gathers every image block in the request input, in order.
func collectImageBlocks(request canonical.Request) []canonical.ImageContent {
	var images []canonical.ImageContent
	for _, item := range request.Input() {
		message, ok := item.Message()
		if !ok {
			continue
		}
		for _, block := range message.Content() {
			if image, ok := block.Image(); ok {
				images = append(images, image)
			}
		}
	}
	return images
}

// targetSeesImages reports whether the target model resolves and declares the
// image_input capability. An unresolvable target defers to the registry so
// clients keep the canonical invalid_model error.
func (b *VisionBridge) targetSeesImages(ctx context.Context, alias string) bool {
	model, found := b.lookupModel(ctx, alias)
	return found && model.Capabilities().Has(canonical.CapabilityImageInput)
}

func (b *VisionBridge) lookupModel(ctx context.Context, alias string) (Model, bool) {
	catalog, ok := b.inner.(ModelCatalog)
	if !ok {
		return Model{}, false
	}
	models, err := catalog.Models(ctx)
	if err != nil {
		return Model{}, false
	}
	for _, model := range models {
		if model.ID() == alias {
			return model, true
		}
	}
	return Model{}, false
}

// describe runs one vision sub-call over all collected images and returns the
// description text produced by the vision model.
func (b *VisionBridge) describe(ctx context.Context, request canonical.Request, images []canonical.ImageContent) (string, error) {
	if _, found := b.lookupModel(ctx, b.bridgeModel); !found {
		return "", NewError(CodeInvalidModel, "data_plane.vision_bridge", 0, nil)
	}
	instruction, err := canonical.NewTextContent(visionBridgePrompt)
	if err != nil {
		return "", NewError(CodeInternal, "vision_bridge.prompt", 0, err)
	}
	blocks := make([]canonical.ContentBlock, 0, len(images)+1)
	instructionBlock, err := canonical.NewTextBlock(instruction)
	if err != nil {
		return "", NewError(CodeInternal, "vision_bridge.prompt", 0, err)
	}
	blocks = append(blocks, instructionBlock)
	for _, image := range images {
		block, err := canonical.NewImageBlock(image)
		if err != nil {
			return "", NewError(CodeInternal, "vision_bridge.image", 0, err)
		}
		blocks = append(blocks, block)
	}
	itemID, err := b.ids.ItemID()
	if err != nil {
		return "", NewError(CodeInternal, "vision_bridge.id", 0, err)
	}
	message, err := canonical.NewMessage(itemID, canonical.Role("user"), blocks)
	if err != nil {
		return "", NewError(CodeInternal, "vision_bridge.message", 0, err)
	}
	item, err := canonical.NewMessageItem(message)
	if err != nil {
		return "", NewError(CodeInternal, "vision_bridge.item", 0, err)
	}
	describeRequest, err := canonical.NewRequest(canonical.RequestParams{ID: request.ID(), Model: b.bridgeModel, Input: []canonical.Item{item}})
	if err != nil {
		return "", NewError(CodeInternal, "vision_bridge.request", 0, err)
	}
	source, err := b.inner.Execute(ctx, describeRequest)
	if err != nil {
		return "", err
	}
	defer func() { _ = source.Close() }()
	text, err := collectResponseText(ctx, source)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(text) == "" {
		return "", NewError(CodeUnavailable, "vision_bridge.description", 0, nil)
	}
	return text, nil
}

// rebuildWithoutImages returns a request whose image blocks are replaced by
// one text block carrying described at the position of the first image;
// later image blocks are removed. Non-image content and non-message items
// are preserved.
func rebuildWithoutImages(request canonical.Request, described string) (canonical.Request, error) {
	description, err := canonical.NewTextContent(described)
	if err != nil {
		return canonical.Request{}, err
	}
	descriptionBlock, err := canonical.NewTextBlock(description)
	if err != nil {
		return canonical.Request{}, err
	}
	injected := false
	input := make([]canonical.Item, 0, len(request.Input()))
	for _, item := range request.Input() {
		message, ok := item.Message()
		if !ok {
			input = append(input, item)
			continue
		}
		blocks := make([]canonical.ContentBlock, 0, len(message.Content()))
		for _, block := range message.Content() {
			if _, isImage := block.Image(); !isImage {
				blocks = append(blocks, block)
				continue
			}
			if !injected {
				blocks = append(blocks, descriptionBlock)
				injected = true
			}
		}
		if len(blocks) == 0 {
			continue
		}
		rebuilt, err := canonical.NewMessage(message.ID(), message.Role(), blocks)
		if err != nil {
			return canonical.Request{}, err
		}
		item, err := canonical.NewMessageItem(rebuilt)
		if err != nil {
			return canonical.Request{}, err
		}
		input = append(input, item)
	}
	return canonical.NewRequest(canonical.RequestParams{
		ID: request.ID(), Model: request.Model(), Input: input,
		Tools: request.Tools(), ToolChoice: request.ToolChoice(),
		Output: request.Output(), Sampling: request.Sampling(),
		ReasoningSummary: request.ReasoningSummary(),
		Continuation:     request.Continuation(),
	})
}

// collectResponseText pumps one canonical stream to completion and returns
// the assistant text of the reconstructed response.
func collectResponseText(ctx context.Context, source streaming.EventSource) (string, error) {
	machine, err := streaming.NewMachine(streaming.DefaultMachineOptions())
	if err != nil {
		return "", NewError(CodeInternal, "vision_bridge.machine", 0, err)
	}
	pump, err := streaming.NewPump(machine, source, discardSink{}, streaming.DefaultPumpOptions())
	if err != nil {
		return "", NewError(CodeInternal, "vision_bridge.pump", 0, err)
	}
	result := pump.Run(ctx)
	if result.Cause() != nil {
		return "", NewError(CodeUnavailable, "vision_bridge.describe", 0, result.Cause())
	}
	response, ok := result.Response()
	if !ok {
		return "", NewError(CodeUnavailable, "vision_bridge.describe", 0, nil)
	}
	text := ""
	for _, item := range response.Items() {
		message, ok := item.Message()
		if !ok {
			continue
		}
		for _, block := range message.Content() {
			if value, ok := block.Text(); ok {
				if text != "" {
					text += "\n"
				}
				text += value.Text()
			}
		}
	}
	return text, nil
}

type discardSink struct{}

func (discardSink) Send(context.Context, canonical.Event) error { return nil }

// AffinityResolver is the structural affinity contract (mirrored by the
// dataplane's request runtime) so the bridge can wrap a full request runtime
// without importing wire packages.
type AffinityResolver interface {
	ResolveModel(alias string) (providerID string, upstreamModel string, err error)
}

// ResolveModel delegates affinity resolution to the inner runtime.
func (b *VisionBridge) ResolveModel(alias string) (string, string, error) {
	if b == nil || b.inner == nil {
		return "", "", NewError(CodeInternal, "vision_bridge", 0, nil)
	}
	resolver, ok := b.inner.(AffinityResolver)
	if !ok {
		return "", "", NewError(CodeInternal, "vision_bridge.affinity", 0, nil)
	}
	return resolver.ResolveModel(alias)
}

var _ Executor = (*VisionBridge)(nil)
