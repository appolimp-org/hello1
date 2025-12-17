package integrationtests

import (
	"common/testutils/assertjson"
	"gitcore/internal/httpserver/httperrors"
	"gitcore/internal/httpserver/schemas"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func (suite *RepoApiTestSuite) TestListRepos() {
	t := suite.T()

	t.Run("OK", func(t *testing.T) {
		details := &schemas.Collection[schemas.RepoDetails]{}

		resp, err := suite.client.As(suite.users.Pikachu.Identity).
			SetResult(details).
			Get("/api/v1/orgs/yandex/repos")

		require.NoError(t, err)
		require.Equal(t, 200, resp.StatusCode())
		require.NotEmpty(t, details.Result)
	})
}

func (suite *RepoApiTestSuite) TestDetails() {
	t := suite.T()

	tests := []struct {
		name             string
		repo             string
		expectedCode     int
		expectedResponse string
		expectedErrCode  httperrors.ErrorCode
	}{
		{
			name:         "ok",
			repo:         "yandex/history",
			expectedCode: http.StatusOK,
			expectedResponse: `{
			    "id": "<<PRESENCE>>",
				"name":"history",
				"slug":"history",
				"orgSlug":"yandex",
				"projSlug":null,
				"defaultBranch":"master",
				"visibility": "public",
				"empty": false,
				"cloneURL":{
					"https":"http://git@localhost:8081/yandex/history.git",
					"ssh":"ssh://localhost:2222/yandex/history.git"
				},
				"description": "description is history",
				"logoURL":null
			}`,
		},
		{
			name:            "NotFoundOrg",
			repo:            "not-found/history",
			expectedCode:    http.StatusNotFound,
			expectedErrCode: httperrors.ErrorCodeNotFoundOrganization,
		},
		{
			name:            "NotFoundRepo",
			repo:            "yandex/not-found",
			expectedCode:    http.StatusNotFound,
			expectedErrCode: httperrors.ErrorCodeNotFoundRepository,
		},
		{
			name:            "BadRequest",
			repo:            "!!!/history",
			expectedCode:    http.StatusBadRequest,
			expectedErrCode: httperrors.ErrorCodeValidationFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			httpErr := httperrors.APIError{}

			resp, err := suite.client.R().
				SetError(&httpErr).
				Get("/api/v1/repos/" + tt.repo)

			require.NoError(t, err)
			require.Equal(t, tt.expectedCode, resp.StatusCode(), string(resp.Body()))

			if tt.expectedResponse != "" {
				assertjson.MatchExact(t, resp.Body(), tt.expectedResponse)
			}
			if tt.expectedErrCode != "" {
				require.Equal(t, string(tt.expectedErrCode), httpErr.ErrorCode)
			}
		})
	}
}
