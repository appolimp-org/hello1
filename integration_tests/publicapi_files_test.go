package integrationtests

import (
	"common/testutils/yarequire"
	"testing"

	"github.com/stretchr/testify/require"
)

func (suite *RwApiTestSuite) TestPublicAPIGetFile() {
	t := suite.T()
	t.Skip("disabled for now -- cdn will be designed later")

	t.Run("get file content", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Admin.Identity).
			SetQueryParam("path", "LICENSE").
			SetQueryParam("rev", "branch").
			SetQueryParam("from", "14").
			SetQueryParam("to", "16").
			Get("/repos/yandex/alpha/fileContent")

		yarequire.StatusCode(t, resp, err, 200)
		expectedReponse := "THE SOFTWARE IS PROVIDED \"AS IS\", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR\nIMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,"
		require.Equal(t, expectedReponse, string(resp.Body()))
	})

	t.Run("get file content with invalid slug", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Admin.Identity).
			SetQueryParam("path", "LICENSE").
			SetQueryParam("rev", "branch").
			SetQueryParam("from", "14").
			SetQueryParam("to", "16").
			Get("/repos/!/!/fileContent")

		yarequire.StatusCode(t, resp, err, 400)
		yarequire.HTTPCompareWithFixture(t, resp, "**/request_id")
	})

	t.Run("get raw", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Admin.Identity).
			SetQueryParam("path", "vendor/foo.go").
			Get("/repos/yandex/alpha/raw")

		yarequire.StatusCode(t, resp, err, 200)
		expectedReponse := "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"Hello, playground\")\n}\n"
		require.Equal(t, expectedReponse, string(resp.Body()))
	})

	t.Run("get raw with invalid slug", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Admin.Identity).
			SetQueryParam("path", "vendor/foo.go").
			Get("/repos/!/!/raw")

		yarequire.StatusCode(t, resp, err, 400)
		yarequire.HTTPCompareWithFixture(t, resp, "**/request_id")
	})
}
