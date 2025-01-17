package artifactory

import (
	"context"
	"regexp"
	"testing"

	"github.com/hashicorp/vault/sdk/logical"
	"github.com/jarcoal/httpmock"
	"github.com/stretchr/testify/assert"
)

func TestAcceptanceBackend_PathConfig(t *testing.T) {
	if !runAcceptanceTests {
		t.SkipNow()
	}

	accTestEnv, err := newAcceptanceTestEnv()
	if err != nil {
		t.Fatal(err)
	}

	t.Run("notConfigured", accTestEnv.PathConfigReadUnconfigured)
	t.Run("update", accTestEnv.UpdatePathConfig)
	t.Run("read", accTestEnv.ReadPathConfig)
	t.Run("expiringTokens", accTestEnv.PathConfigUpdateExpiringTokens)
	t.Run("bypassArtifactoryTLSVerification", accTestEnv.PathConfigUpdateBypassArtifactoryTLSVerification)
	t.Run("usernameTemplate", accTestEnv.PathConfigUpdateUsernameTemplate)
	t.Run("delete", accTestEnv.DeletePathConfig)
	t.Run("errors", accTestEnv.PathConfigUpdateErrors)
	t.Run("badAccessToken", accTestEnv.PathConfigReadBadAccessToken)
}

func (e *accTestEnv) PathConfigReadUnconfigured(t *testing.T) {
	resp, err := e.read("config/admin")
	assert.Contains(t, resp.Data["error"], "backend not configured")
	assert.NoError(t, err)
}

func (e *accTestEnv) PathConfigUpdateExpiringTokens(t *testing.T) {
	e.pathConfigUpdateBooleanField(t, "use_expiring_tokens")
}

func (e *accTestEnv) PathConfigUpdateBypassArtifactoryTLSVerification(t *testing.T) {
	e.pathConfigUpdateBooleanField(t, "bypass_artifactory_tls_verification")
}

func (e *accTestEnv) pathConfigUpdateBooleanField(t *testing.T, fieldName string) {
	// Boolean
	e.UpdateConfigAdmin(t, testData{
		fieldName: true,
	})
	data := e.ReadConfigAdmin(t)
	assert.Equal(t, true, data[fieldName])

	e.UpdateConfigAdmin(t, testData{
		fieldName: false,
	})
	data = e.ReadConfigAdmin(t)
	assert.Equal(t, false, data[fieldName])

	// String
	e.UpdateConfigAdmin(t, testData{
		fieldName: "true",
	})
	data = e.ReadConfigAdmin(t)
	assert.Equal(t, true, data[fieldName])

	e.UpdateConfigAdmin(t, testData{
		fieldName: "false",
	})
	data = e.ReadConfigAdmin(t)
	assert.Equal(t, false, data[fieldName])

	// Fail Tests
	resp, err := e.update("config/admin", testData{
		fieldName: "Sure, why not",
	})
	assert.NotNil(t, resp)
	assert.Regexp(t, regexp.MustCompile("Field validation failed: error converting input .* strconv.ParseBool: parsing .*: invalid syntax"), resp.Data["error"])
	assert.Nil(t, err)
}

func (e *accTestEnv) PathConfigUpdateUsernameTemplate(t *testing.T) {
	usernameTemplate := "v_{{.DisplayName}}_{{.RoleName}}_{{random 10}}_{{unix_time}}"
	e.UpdateConfigAdmin(t, testData{
		"username_template": usernameTemplate,
	})
	data := e.ReadConfigAdmin(t)
	assert.Equal(t, data["username_template"], usernameTemplate)

	// Bad Template
	resp, err := e.update("config/admin", testData{
		"username_template": "bad_{{ .somethingInvalid }}_testing {{",
	})
	assert.NotNil(t, resp)
	assert.Contains(t, resp.Data["error"], "username_template error")
	assert.ErrorContains(t, err, "username_template")
}

// most of these were covered by unit tests, but we want test coverage for acceptance
func (e *accTestEnv) PathConfigUpdateErrors(t *testing.T) {
	// Access Token Required
	resp, err := e.update("config/admin", testData{
		"url": e.URL,
	})
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.True(t, resp.IsError())
	assert.Contains(t, resp.Error().Error(), "access_token")
	// URL Required
	resp, err = e.update("config/admin", testData{
		"access_token": "test-access-token",
	})
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.True(t, resp.IsError())
	assert.Contains(t, resp.Error().Error(), "url")
	// Bad Token
	resp, err = e.update("config/admin", testData{
		"access_token": "test-access-token",
		"url":          e.URL,
	})
	assert.NotNil(t, resp)
	assert.True(t, resp.IsError())
	assert.Contains(t, resp.Error().Error(), "Unable to get Artifactory Version")
	assert.ErrorContains(t, err, "could not get the system version")
}

func (e *accTestEnv) PathConfigReadBadAccessToken(t *testing.T) {
	// Forcibly set a bad token
	entry, err := logical.StorageEntryJSON("config/admin", adminConfiguration{
		AccessToken:    "bogus.token",
		ArtifactoryURL: e.URL,
	})
	assert.NoError(t, err)
	err = e.Storage.Put(e.Context, entry)
	assert.NoError(t, err)
	resp, err := e.read("config/admin")

	assert.NoError(t, err)
	assert.NotNil(t, resp)
	// Otherwise success, we don't need to re-test for this
}

func TestBackend_AccessTokenRequired(t *testing.T) {
	b, config := makeBackend(t)

	adminConfig := map[string]interface{}{
		"url": "https://127.0.0.1",
	}

	resp, err := b.HandleRequest(context.Background(), &logical.Request{
		Operation: logical.UpdateOperation,
		Path:      "config/admin",
		Storage:   config.StorageView,
		Data:      adminConfig,
	})
	assert.NoError(t, err)

	assert.NotNil(t, resp)
	assert.True(t, resp.IsError())
	assert.Contains(t, resp.Error().Error(), "access_token")
}

func TestBackend_URLRequired(t *testing.T) {
	b, config := makeBackend(t)

	adminConfig := map[string]interface{}{
		"access_token": "test-access-token",
	}

	resp, err := b.HandleRequest(context.Background(), &logical.Request{
		Operation: logical.UpdateOperation,
		Path:      "config/admin",
		Storage:   config.StorageView,
		Data:      adminConfig,
	})
	assert.NoError(t, err)

	assert.NotNil(t, resp)
	assert.True(t, resp.IsError())
	assert.Contains(t, resp.Error().Error(), "url")
}

// When requesting the config, the access_token must be returned sha256 encoded.
// echo -n "test-access-token"  | shasum -a 256
// 597480d4b62ca612193f19e73fe4cc3ad17f0bf9cfc16a7cbf4b5064131c4805  -
func TestBackend_AccessTokenAsSHA256(t *testing.T) {

	const correctSHA256 = "597480d4b62ca612193f19e73fe4cc3ad17f0bf9cfc16a7cbf4b5064131c4805"
	httpmock.Activate()
	defer httpmock.DeactivateAndReset()

	mockArtifactoryUsageVersionRequests("")

	httpmock.RegisterResponder(
		"GET",
		"http://myserver.com:80/access/api/v1/cert/root",
		httpmock.NewStringResponder(200, rootCert))

	b, config := configuredBackend(t, map[string]interface{}{
		"access_token": "test-access-token",
		"url":          "http://myserver.com:80",
	})

	resp, err := b.HandleRequest(context.Background(), &logical.Request{
		Operation: logical.ReadOperation,
		Path:      "config/admin",
		Storage:   config.StorageView,
	})

	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.EqualValues(t, correctSHA256, resp.Data["access_token_sha256"])
}
