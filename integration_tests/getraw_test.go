package integrationtests

import (
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
)

func (suite *RepoApiTestSuite) TestGetRaw() {
	t := suite.T()

	t.Run("by path", func(t *testing.T) {
		resp, err := suite.client.R().
			SetQueryParam("path", "1.txt").
			Get("/api/v1/repos/yandex/history/raw")

		require.NoError(t, err)
		require.Equal(t, "attachment; filename=1.txt", resp.Header().Get(echo.HeaderContentDisposition))
		require.Equal(t, "3 initial (master-top)\n", string(resp.Body()))
		require.Equal(t, 200, resp.StatusCode())

		// request twice to get cached
		resp, err = suite.client.R().
			SetQueryParam("path", "1.txt").
			Get("/api/v1/repos/yandex/history/raw")

		require.NoError(t, err)
		require.Equal(t, "attachment; filename=1.txt", resp.Header().Get(echo.HeaderContentDisposition))
		require.Equal(t, "3 initial (master-top)\n", string(resp.Body()))
		require.Equal(t, 200, resp.StatusCode())
	})

	t.Run("by path, missing", func(t *testing.T) {
		resp, err := suite.client.R().
			SetQueryParam("path", "2.txt").
			Get("/api/v1/repos/yandex/history/raw")

		require.NoError(t, err)

		require.Equal(t, 404, resp.StatusCode())
	})
	t.Run("by path, deep", func(t *testing.T) {
		resp, err := suite.client.R().
			SetQueryParam("path", "vendor/foo.go").
			Get("/api/v1/repos/yandex/alpha/raw")

		require.NoError(t, err)

		require.Equal(t, 200, resp.StatusCode())
		require.Equal(t, "attachment; filename=\"vendor/foo.go\"", resp.Header().Get(echo.HeaderContentDisposition))
	})

	t.Run("by path, deep", func(t *testing.T) {
		resp, err := suite.client.R().
			SetQueryParam("path", "../../../etc/passwd").
			Get("/api/v1/repos/yandex/history/raw")

		require.NoError(t, err)
		require.Equal(t, 400, resp.StatusCode())
	})
	t.Run("by path, deep", func(t *testing.T) {
		resp, err := suite.client.R().
			SetQueryParam("path", "etc/passwd").
			Get("/api/v1/repos/yandex/history/raw")

		require.NoError(t, err)
		require.Equal(t, 404, resp.StatusCode())
	})
	t.Run("empty", func(t *testing.T) {
		resp, err := suite.client.R().
			Get("/api/v1/repos/yandex/history/raw")

		require.NoError(t, err)
		require.Equal(t, 400, resp.StatusCode())
	})

	t.Run("by path, rev-branch", func(t *testing.T) {
		resp, err := suite.client.R().
			SetQueryParam("path", "1.txt").
			SetQueryParam("rev", "b1").
			Get("/api/v1/repos/yandex/history/raw")

		require.NoError(t, err)
		require.Equal(t, "3 initial (b1-top)\n", string(resp.Body()))
		require.Equal(t, 200, resp.StatusCode())
		require.Equal(t, "attachment; filename=1.txt", resp.Header().Get(echo.HeaderContentDisposition))
	})
	t.Run("by sha", func(t *testing.T) {
		resp, err := suite.client.R().
			SetQueryParam("sha", "c6b0000cc556725837d1ebdbd979223f0bcb02b5").
			Get("/api/v1/repos/yandex/history/raw")
		require.NoError(t, err)
		require.Equal(t, 400, resp.StatusCode())
	})
	t.Run("by path, rev-sha", func(t *testing.T) {
		resp, err := suite.client.R().
			SetQueryParam("path", "1.txt").
			SetQueryParam("rev", "1ccd97f1bc3277c234d03a6687973d27b6ed765e").
			Get("/api/v1/repos/yandex/history/raw")

		require.NoError(t, err)
		require.Equal(t, "2 initial\n", string(resp.Body()))
		require.Equal(t, 200, resp.StatusCode())
		require.Equal(t, "attachment; filename=1.txt", resp.Header().Get(echo.HeaderContentDisposition))
	})

	t.Run("by path, rev - missing", func(t *testing.T) {
		resp, err := suite.client.R().
			SetQueryParam("path", "1.txt").
			SetQueryParam("rev", "missing_branch").
			Get("/api/v1/repos/yandex/history/raw")

		require.NoError(t, err)
		require.Equal(t, 404, resp.StatusCode())
	})

	t.Run("by sha commit", func(t *testing.T) {
		resp, err := suite.client.R().
			SetQueryParam("sha", "6e35d3ad15a331799f25b117ff502ef598f636f5").
			Get("/api/v1/repos/yandex/history/raw")

		require.NoError(t, err)
		require.Equal(t, 400, resp.StatusCode())
	})
}
