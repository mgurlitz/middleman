package telemetry

import (
	"os"
	"testing"

	"github.com/posthog/posthog-go"
	Assert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.kenn.io/middleman/internal/testutil/dbtest"
)

type fakePostHogClient struct {
	message posthog.Message
}

func (f *fakePostHogClient) Enqueue(message posthog.Message) error {
	f.message = message
	return nil
}

func (f *fakePostHogClient) Close() error { return nil }

func TestEnabledFromEnv(t *testing.T) {
	assert := Assert.New(t)

	original, hadOriginal := os.LookupEnv(EnabledEnv)
	t.Cleanup(func() {
		if hadOriginal {
			require.NoError(t, os.Setenv(EnabledEnv, original))
			return
		}
		require.NoError(t, os.Unsetenv(EnabledEnv))
	})

	require.NoError(t, os.Unsetenv(EnabledEnv))
	assert.False(EnabledFromEnv())

	cases := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "one", value: "1", want: true},
		{name: "true", value: "true", want: true},
		{name: "yes", value: "yes", want: true},
		{name: "on", value: "on", want: true},
		{name: "zero", value: "0", want: false},
		{name: "empty", value: "", want: false},
		{name: "other", value: "maybe", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, os.Setenv(EnabledEnv, tc.value))
			Assert.Equal(t, tc.want, EnabledFromEnv())
		})
	}
}

func TestNewReporterDisabledByDefaultDoesNotCreateInstallID(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)

	original, hadOriginal := os.LookupEnv(EnabledEnv)
	t.Cleanup(func() {
		if hadOriginal {
			require.NoError(os.Setenv(EnabledEnv, original))
			return
		}
		require.NoError(os.Unsetenv(EnabledEnv))
	})
	require.NoError(os.Unsetenv(EnabledEnv))

	database := dbtest.Open(t)

	reporter, err := NewReporter(Options{Database: database})
	require.NoError(err)

	assert.False(reporter.Enabled())
	_, found, err := database.AppMetadataValue(t.Context(), installIDMetadataKey)
	require.NoError(err)
	assert.False(found)
}

func TestNewReporterDisabledByEnvDoesNotCreateInstallID(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)

	t.Setenv(EnabledEnv, "0")
	database := dbtest.Open(t)

	reporter, err := NewReporter(Options{Database: database})
	require.NoError(err)

	assert.False(reporter.Enabled())
	_, found, err := database.AppMetadataValue(t.Context(), installIDMetadataKey)
	require.NoError(err)
	assert.False(found)
}

func TestLoadOrCreateInstallIDIsStableAndAnonymous(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)

	database := dbtest.Open(t)

	first, err := loadOrCreateInstallID(t.Context(), database)
	require.NoError(err)
	second, err := loadOrCreateInstallID(t.Context(), database)
	require.NoError(err)

	assert.Len(first, 32)
	assert.Equal(first, second)

	stored, found, err := database.AppMetadataValue(t.Context(), installIDMetadataKey)
	require.NoError(err)
	assert.True(found)
	assert.Equal(first, stored)
}

func TestReporterCaptureUsesAnonymousDistinctID(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)

	client := &fakePostHogClient{}
	reporter := &Reporter{
		client:     client,
		distinctID: "anonymous-install-id",
		enabled:    true,
	}

	err := reporter.Capture("app_loaded", map[string]any{
		"$geoip_disable": false,
		"distinct_id":    "user-provided",
		"repo":           "owner/name",
		"view":           "pulls",
	})
	require.NoError(err)

	capture, ok := client.message.(posthog.Capture)
	require.True(ok)
	assert.Equal("anonymous-install-id", capture.DistinctId)
	assert.Equal("app_loaded", capture.Event)
	assert.Equal("pulls", capture.Properties["view"])
	assert.NotContains(capture.Properties, "distinct_id")
	assert.NotContains(capture.Properties, "repo")
	assert.True(capture.Properties["$geoip_disable"].(bool))
}

func TestReporterCaptureRejectsUnsupportedEvents(t *testing.T) {
	require := require.New(t)

	client := &fakePostHogClient{}
	reporter := &Reporter{
		client:     client,
		distinctID: "anonymous-install-id",
		enabled:    true,
	}

	err := reporter.Capture("repo_opened", map[string]any{"view": "pulls"})
	require.ErrorIs(err, ErrUnsupportedEvent)
}

func TestReporterCaptureDropsUnsafePropertyValues(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)

	client := &fakePostHogClient{}
	reporter := &Reporter{
		client:     client,
		distinctID: "anonymous-install-id",
		enabled:    true,
	}

	err := reporter.Capture("app_loaded", map[string]any{"view": "owner/repo"})
	require.NoError(err)

	capture, ok := client.message.(posthog.Capture)
	require.True(ok)
	assert.NotContains(capture.Properties, "view")
	assert.True(capture.Properties["$geoip_disable"].(bool))
}
