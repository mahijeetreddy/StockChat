package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadDotEnvSkipsMissingFiles(t *testing.T) {
	dir := t.TempDir()
	parent := filepath.Join(dir, ".env")
	require.NoError(t, os.WriteFile(parent, []byte("SC_TEST_A=from-parent\r\nSC_TEST_B=parent\nSC_TEST_D=                # optional token\nSC_TEST_E=value   # trailing comment\n"), 0o600))
	local := filepath.Join(dir, "sub", ".env")
	require.NoError(t, os.MkdirAll(filepath.Dir(local), 0o750))
	require.NoError(t, os.WriteFile(local, []byte("SC_TEST_B=local\n"), 0o600))

	t.Setenv("SC_TEST_A", "")
	require.NoError(t, os.Unsetenv("SC_TEST_A"))
	t.Setenv("SC_TEST_B", "")
	require.NoError(t, os.Unsetenv("SC_TEST_B"))
	t.Setenv("SC_TEST_C", "real-env")
	for _, k := range []string{"SC_TEST_D", "SC_TEST_E"} {
		t.Setenv(k, "")
		require.NoError(t, os.Unsetenv(k))
	}

	missing := filepath.Join(dir, "nope", ".env")
	require.NoError(t, LoadDotEnv(missing, local, parent))

	assert.Equal(t, "from-parent", os.Getenv("SC_TEST_A"), "a missing first file must not stop loading")
	assert.Equal(t, "local", os.Getenv("SC_TEST_B"), "earlier files win")
	assert.Equal(t, "real-env", os.Getenv("SC_TEST_C"), "real env vars are never overridden")
	v, set := os.LookupEnv("SC_TEST_D")
	assert.True(t, set)
	assert.Empty(t, v, "an empty value with a trailing comment is empty, not the comment text")
	assert.Equal(t, "value", os.Getenv("SC_TEST_E"))
}
