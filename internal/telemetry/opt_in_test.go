package telemetry

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.kenn.io/forge/internal/testutil/dbtest"
)

func TestEnabledFromEnvRequiresOptIn(t *testing.T) {
	for _, tc := range []struct {
		name, generic, prefixed string
		want                    bool
	}{
		{name: "default"},
		{name: "generic opt in", generic: "1", want: true},
		{name: "prefixed opt in", prefixed: "true", want: true},
		{name: "trimmed case insensitive", generic: " YES ", want: true},
		{name: "on", generic: "on", want: true},
		{name: "invalid", generic: "maybe"},
		{name: "generic veto", generic: "0", prefixed: "1"},
		{name: "prefixed veto", generic: "1", prefixed: "off"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(EnabledEnv, tc.generic)
			t.Setenv("KENN_FORGE_TELEMETRY_ENABLED", tc.prefixed)
			assert.Equal(t, tc.want, EnabledFromEnv())
		})
	}
}

func TestDefaultDisabledReporterDoesNotCreateInstallID(t *testing.T) {
	t.Setenv(EnabledEnv, "")
	t.Setenv("KENN_FORGE_TELEMETRY_ENABLED", "")
	database := dbtest.Open(t)
	reporter, err := newReporter(Options{Database: database}, time.Now())
	require.NoError(t, err)
	assert.False(t, reporter.Enabled())
	_, found, err := database.AppMetadataValue(t.Context(), InstallIDMetadataKey)
	require.NoError(t, err)
	assert.False(t, found)
}
