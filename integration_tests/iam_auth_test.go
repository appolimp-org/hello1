package integrationtests

import (
	"gitcore/internal/httpserver/httperrors"
	"testing"

	"github.com/stretchr/testify/require"
)

func (suite *RepoApiTestSuite) TestAuth() {
	httpErr := httperrors.APIError{}

	r := suite.client.R()

	t := suite.T()

	t.Run("no token", func(t *testing.T) {
		r = r.SetError(&httpErr)
		r.Header.Del("Authorization")
		resp, err := r.Get("/api/v1/me")

		require.NoError(t, err)
		require.Equal(t, 200, resp.StatusCode())

		// it is okay to go as guest to public endpoints
		resp, err = r.Get("/api/v1/repos/yandex/alpha")
		require.NoError(t, err)
		require.Equal(t, 200, resp.StatusCode())
	})

	t.Run("bad token (refused by IAM)", func(t *testing.T) {
		r = r.SetError(&httpErr)
		r.SetHeader("Authorization", "Bearer t1.badtoken")
		resp, err := r.Get("/api/v1/me")
		require.NoError(t, err)
		require.Equal(t, 401, resp.StatusCode())
		require.Equal(t, httperrors.ErrUnauthorized.ErrorCode, httpErr.ErrorCode)

		// bad token triggers 401 even on endpoints that are public for guests
		resp, err = r.Get("/api/v1/repos/yandex/alpha")
		require.NoError(t, err)
		require.Equal(t, 401, resp.StatusCode())
	})

	t.Run("bad token (refused by us)", func(t *testing.T) {
		r = r.SetError(&httpErr)
		r.SetHeader("Authorization", "ahahahahahahahah") // no prefix, corrupt
		resp, err := r.Get("/api/v1/me")
		require.NoError(t, err)
		require.Equal(t, 401, resp.StatusCode())
		require.Equal(t, httperrors.ErrUnauthorized.ErrorCode, httpErr.ErrorCode)

		// bad token triggers 401 even on endpoints that are public for guests
		resp, err = r.Get("/api/v1/repos/yandex/alpha")
		require.NoError(t, err)
		require.Equal(t, 401, resp.StatusCode())
	})

	t.Run("ok", func(t *testing.T) {
		resp, err := suite.client.R().Get("/api/v1/me")

		require.NoError(t, err)
		require.Equal(t, 200, resp.StatusCode())
	})
}
