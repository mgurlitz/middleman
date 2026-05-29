package platform

// SupportsLocalClone reports whether middleman should attempt local bare-clone,
// diff, and workspace flows for the provider.
func SupportsLocalClone(_ Kind) bool {
	return true
}
