//go:build unit

package common

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveRepoURL(t *testing.T) {
	tests := []struct {
		name    string
		remote  string
		gitErr  error
		envs    map[string]string
		want    string
		wantErr bool
	}{
		{
			name:   "remote wins over the env var",
			remote: "https://github.com/org/from-git.git\n",
			envs:   map[string]string{"GIT_REPOSITORY_URL": "https://github.com/org/from-env.git"},
			want:   "https://github.com/org/from-git.git",
		},
		{
			name:    "git failure falls back to the env var",
			gitErr:  errors.New("exit status 1"),
			envs:    map[string]string{"GIT_REPOSITORY_URL": "https://github.com/org/from-env.git"},
			want:    "https://github.com/org/from-env.git",
			wantErr: true,
		},
		{
			name:   "a remote-less repo falls back to the env var",
			remote: "\n",
			envs:   map[string]string{"GIT_REPOSITORY_URL": "https://github.com/org/from-env.git"},
			want:   "https://github.com/org/from-env.git",
		},
		{
			name:    "no source at all",
			gitErr:  errors.New("exit status 1"),
			envs:    map[string]string{},
			want:    "",
			wantErr: true,
		},
		{
			name:   "credentials are stripped from the remote",
			remote: "https://x-access-token:ghs_secretsecret@github.com/org/repo.git",
			want:   "https://github.com/org/repo.git",
		},
		{
			name:   "a token-as-username is stripped too",
			remote: "https://ghp_secretsecret@github.com/org/repo.git",
			want:   "https://github.com/org/repo.git",
		},
		{
			name:   "credentials are stripped from the env var",
			gitErr: errors.New("exit status 1"),
			envs:   map[string]string{"GIT_REPOSITORY_URL": "https://user:pat@github.com/org/repo.git"},
			want:   "https://github.com/org/repo.git",
			// The git failure is still reported.
			wantErr: true,
		},
		{
			name:   "an scp-form remote is left alone",
			remote: "git@github.com:org/repo.git",
			want:   "git@github.com:org/repo.git",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			commandFunc := func(string, ...string) (string, error) {
				return tt.remote, tt.gitErr
			}

			got, err := ResolveRepoURL(commandFunc, tt.envs)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, tt.want, got)
		})
	}
}
