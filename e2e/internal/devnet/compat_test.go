package devnet

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// stubFetch replaces the API call for one test and records whether it ran.
func stubFetch(t *testing.T, tags []string) *bool {
	t.Helper()
	called := false
	orig := fetchClientReleaseTags
	fetchClientReleaseTags = func(context.Context, string) ([]string, error) {
		called = true
		return tags, nil
	}
	t.Cleanup(func() { fetchClientReleaseTags = orig })
	return &called
}

func writeFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "releases.json")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func TestClientReleaseTags_FilePresent(t *testing.T) {
	called := stubFetch(t, nil)
	path := writeFile(t, `["client/v0.10.0","controller/v0.10.0","client/v0.9.1","monitor/v0.50.0"]`)
	t.Setenv(ClientReleasesFileEnv, path)

	tags, source, err := ClientReleaseTags(t.Context(), "")
	require.NoError(t, err)
	require.Equal(t, []string{"v0.10.0", "v0.9.1"}, tags)
	require.Equal(t, "file "+path, source)
	require.False(t, *called, "the API must not be called when the file is set")
}

func TestClientReleaseTags_FileAbsentUsesAPI(t *testing.T) {
	called := stubFetch(t, []string{"v0.10.0"})
	t.Setenv(ClientReleasesFileEnv, "")

	tags, source, err := ClientReleaseTags(t.Context(), "")
	require.NoError(t, err)
	require.Equal(t, []string{"v0.10.0"}, tags)
	require.Equal(t, "GitHub API", source)
	require.True(t, *called)
}

func TestClientReleaseTags_FileMissingFails(t *testing.T) {
	called := stubFetch(t, nil)
	path := filepath.Join(t.TempDir(), "missing.json")
	t.Setenv(ClientReleasesFileEnv, path)

	_, _, err := ClientReleaseTags(t.Context(), "")
	require.ErrorIs(t, err, os.ErrNotExist)
	require.ErrorContains(t, err, path)
	require.False(t, *called, "a missing file must not fall back to the API")
}

func TestClientReleaseTags_FileMalformedFails(t *testing.T) {
	for name, content := range map[string]string{
		"not json":       `client/v0.10.0`,
		"object":         `{"tag_name":"client/v0.10.0"}`,
		"non-string tag": `[{"tag_name":"client/v0.10.0"}]`,
		"null":           `null`,
		"empty":          ``,
	} {
		t.Run(name, func(t *testing.T) {
			called := stubFetch(t, nil)
			path := writeFile(t, content)
			t.Setenv(ClientReleasesFileEnv, path)

			_, _, err := ClientReleaseTags(t.Context(), "")
			require.Error(t, err)
			require.ErrorContains(t, err, ClientReleasesFileEnv+"="+path)
			require.False(t, *called)
		})
	}
}
