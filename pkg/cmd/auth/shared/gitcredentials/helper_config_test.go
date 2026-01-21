package gitcredentials_test

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/cli/cli/v2/git"
	"github.com/cli/cli/v2/pkg/cmd/auth/shared"
	"github.com/cli/cli/v2/pkg/cmd/auth/shared/contract"
	"github.com/cli/cli/v2/pkg/cmd/auth/shared/gitcredentials"
	"github.com/stretchr/testify/require"
)

// unsetGlobalConfig unsets a global git config key, ignoring errors if the key doesn't exist
func unsetGlobalConfig(gc *git.Client, key string) {
	cmd, err := gc.Command(context.Background(), "config", "--global", "--unset-all", key)
	if err == nil {
		_ = cmd.Run() // Ignore errors - key might not exist
	}
}

func withIsolatedGitConfig(t *testing.T) {
	t.Helper()

	// https://git-scm.com/docs/git-config#ENVIRONMENT
	// Set the global git config to a temporary file
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, ".gitconfig")
	t.Setenv("GIT_CONFIG_GLOBAL", configFile)

	// And disable git reading the system config
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")

	// Change to a directory without a .git folder to avoid reading local config
	// This prevents picking up credential helpers from the repository's .git/config
	originalDir := t.TempDir()
	t.Chdir(originalDir)

	// Disable any credential helpers that might be configured in the environment
	// This prevents the test from picking up actual GitHub credentials
	t.Setenv("GIT_TERMINAL_PROMPT", "0")
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")

	// Explicitly unset any global credential configuration
	gc := &git.Client{}
	unsetGlobalConfig(gc, "credential.username")
	unsetGlobalConfig(gc, "credential.helper")
}

func configureTestCredentialHelper(t *testing.T, key string) {
	t.Helper()

	gc := &git.Client{}
	cmd, err := gc.Command(context.Background(), "config", "--global", "--add", key, "test-helper")
	require.NoError(t, err)
	require.NoError(t, cmd.Run())
}

func TestHelperConfigContract(t *testing.T) {
	contract.HelperConfig{
		NewHelperConfig: func(t *testing.T) shared.HelperConfig {
			withIsolatedGitConfig(t)

			return &gitcredentials.HelperConfig{
				SelfExecutablePath: "/path/to/gh",
				GitClient:          &git.Client{},
			}
		},
		ConfigureHelper: func(t *testing.T, hostname string) {
			configureTestCredentialHelper(t, hostname)
		},
	}.Test(t)
}

// This is a whitebox test unlike the contract because although we don't use the exact configured command, it's
// important that it is exactly right since git uses it.
func TestSetsCorrectCommandInGitConfig(t *testing.T) {
	withIsolatedGitConfig(t)

	gc := &git.Client{}
	hc := &gitcredentials.HelperConfig{
		SelfExecutablePath: "/path/to/gh",
		GitClient:          gc,
	}
	require.NoError(t, hc.ConfigureOurs("github.com"))

	// Check that the correct command was set in the git config
	cmd, err := gc.Command(context.Background(), "config", "--get", "credential.https://github.com.helper")
	require.NoError(t, err)
	output, err := cmd.Output()
	require.NoError(t, err)
	require.Equal(t, "!/path/to/gh auth git-credential\n", string(output))
}

func TestHelperIsOurs(t *testing.T) {
	tests := []struct {
		name        string
		cmd         string
		want        bool
		windowsOnly bool
	}{
		{
			name: "blank",
			cmd:  "",
			want: false,
		},
		{
			name: "invalid",
			cmd:  "!",
			want: false,
		},
		{
			name: "osxkeychain",
			cmd:  "osxkeychain",
			want: false,
		},
		{
			name: "looks like gh but isn't",
			cmd:  "gh auth",
			want: false,
		},
		{
			name: "ours",
			cmd:  "!/path/to/gh auth",
			want: true,
		},
		{
			name:        "ours - Windows edition",
			cmd:         `!'C:\Program Files\GitHub CLI\gh.exe' auth git-credential`,
			want:        true,
			windowsOnly: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.windowsOnly && runtime.GOOS != "windows" {
				t.Skip("skipping test on non-Windows platform")
			}

			h := gitcredentials.Helper{Cmd: tt.cmd}
			if got := h.IsOurs(); got != tt.want {
				t.Errorf("IsOurs() = %v, want %v", got, tt.want)
			}
		})
	}
}
