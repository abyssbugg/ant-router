package canonical

// ProtocolVersionV1 identifies the legacy canonical protocol contract.
const ProtocolVersionV1 = "1"

// ProtocolVersion identifies the current canonical protocol contract.
const ProtocolVersion = "2"

func validProtocolVersion(version string) bool {
	return version == ProtocolVersionV1 || version == ProtocolVersion
}
