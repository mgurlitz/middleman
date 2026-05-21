package platform

// SupportsLocalClone reports whether middleman should attempt local bare-clone,
// diff, and workspace flows for the provider. Azure DevOps is intentionally
// read-only in the current POC, so clone-backed features stay disabled.
func SupportsLocalClone(kind Kind) bool {
	kind, err := NormalizeKind(string(kind))
	if err != nil {
		return true
	}
	switch kind {
	case KindAzureDevOps:
		return false
	default:
		return true
	}
}
