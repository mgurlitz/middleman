package platform

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSupportsLocalCloneAllowsAllProviders(t *testing.T) {
	assert.True(t, SupportsLocalClone(KindGitHub))
	assert.True(t, SupportsLocalClone(KindGitLab))
	assert.True(t, SupportsLocalClone(KindForgejo))
	assert.True(t, SupportsLocalClone(KindGitea))
	assert.True(t, SupportsLocalClone(KindAzureDevOps))
	assert.True(t, SupportsLocalClone(Kind("future_provider")))
}
