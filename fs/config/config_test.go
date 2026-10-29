// These are in an external package because we need to import configfile

package config_test

import (
	"context"
	"testing"

	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/config"
	"github.com/rclone/rclone/fs/config/configfile"
	"github.com/rclone/rclone/fs/config/configmap"
	"github.com/rclone/rclone/fs/rc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	configfile.Install()
}

func TestConfigLoad(t *testing.T) {
	oldConfigPath := config.GetConfigPath()
	assert.NoError(t, config.SetConfigPath("./testdata/plain.conf"))
	defer func() {
		assert.NoError(t, config.SetConfigPath(oldConfigPath))
	}()
	config.ClearConfigPassword()
	sections := config.Data().GetSectionList()
	var expect = []string{"RCLONE_ENCRYPT_V0", "nounc", "unc"}
	assert.Equal(t, expect, sections)

	keys := config.Data().GetKeyList("nounc")
	expect = []string{"type", "nounc"}
	assert.Equal(t, expect, keys)
}

// TestCreateRemoteEphemeralConfigKeysReachBackend checks that config_* parameters
// (#9572) are readable by the backend via the mapper but not saved to the config file.
func TestCreateRemoteEphemeralConfigKeysReachBackend(t *testing.T) {
	defer testConfigFile(t, simpleOptions, "ephemeral.conf")()
	ctx := context.Background()

	var seenTemplateFile, seenTemplate string
	backendName := "config_template_test_remote"
	if regInfo, _ := fs.Find(backendName); regInfo == nil {
		fs.Register(&fs.RegInfo{
			Name: backendName,
			Config: func(_ context.Context, _ string, m configmap.Mapper, _ fs.ConfigIn) (*fs.ConfigOut, error) {
				seenTemplateFile, _ = m.Get("config_template_file")
				seenTemplate, _ = m.Get("config_template")
				return nil, nil
			},
		})
	}

	_, err := config.CreateRemote(ctx, "eph", backendName, rc.Params{
		"config_template_file": "/path/to/template.html",
		"config_template":      "<html>ok</html>",
	}, config.UpdateRemoteOpt{NonInteractive: true})
	require.NoError(t, err)

	// The backend must be able to read the ephemeral config_* parameters
	// through the mapper (before the fix these came back empty).
	assert.Equal(t, "/path/to/template.html", seenTemplateFile)
	assert.Equal(t, "<html>ok</html>", seenTemplate)

	// ...but they must not be persisted to the config file.
	assert.Equal(t, "", config.GetValue("eph", "config_template_file"))
	assert.Equal(t, "", config.GetValue("eph", "config_template"))
}

// TestUpdateRemoteTokenNotRefreshed checks that a token passed to
// UpdateRemote is kept rather than replaced by the OAuth flow.
func TestUpdateRemoteTokenNotRefreshed(t *testing.T) {
	defer testConfigFile(t, simpleOptions, "token.conf")()
	ctx := context.Background()

	var gotRefresh string
	backendName := "config_token_test_remote"
	if regInfo, _ := fs.Find(backendName); regInfo == nil {
		fs.Register(&fs.RegInfo{
			Name: backendName,
			Config: func(_ context.Context, _ string, _ configmap.Mapper, in fs.ConfigIn) (*fs.ConfigOut, error) {
				switch in.State {
				case "":
					return fs.ConfigConfirm("refresh", true, config.ConfigRefreshToken, "Token already configured - replace it?")
				case "refresh":
					gotRefresh = in.Result
				}
				return nil, nil
			},
		})
	}

	_, err := config.CreateRemote(ctx, "tok", backendName, rc.Params{}, config.UpdateRemoteOpt{NonInteractive: true})
	require.NoError(t, err)

	for _, test := range []struct {
		name       string
		keyValues  rc.Params
		wantAsked  bool
		wantResult string
	}{
		{
			name:       "TokenGivenKeepsToken",
			keyValues:  rc.Params{"token": `{"access_token":"a"}`},
			wantResult: "false",
		},
		{
			name:       "TokenGivenExplicitRefresh",
			keyValues:  rc.Params{"token": `{"access_token":"b"}`, config.ConfigRefreshToken: "true"},
			wantResult: "true",
		},
		{
			name:      "NoTokenAsks",
			keyValues: rc.Params{"test_key": "x"},
			wantAsked: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			gotRefresh = ""
			out, err := config.UpdateRemote(ctx, "tok", test.keyValues, config.UpdateRemoteOpt{NonInteractive: true})
			require.NoError(t, err)
			if test.wantAsked {
				require.NotNil(t, out)
				require.NotNil(t, out.Option)
				assert.Equal(t, config.ConfigRefreshToken, out.Option.Name)
				return
			}
			assert.Nil(t, out)
			assert.Equal(t, test.wantResult, gotRefresh)
			assert.Equal(t, test.keyValues["token"], config.GetValue("tok", "token"))
			assert.Equal(t, "", config.GetValue("tok", config.ConfigRefreshToken))
		})
	}
}
