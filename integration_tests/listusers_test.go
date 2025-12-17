package integrationtests

import (
	"common/testutils/assertjson"
	"common/testutils/yarequire"
	"fmt"
	"gitcore/internal/entities"
	"gitcore/internal/httpserver/httperrors"
	"gitcore/internal/testutils"
	"github.com/stretchr/testify/require"
	"strconv"
	"testing"
)

func (suite *RepoApiTestSuite) TestOrgs_ListUsers() {
	t := suite.T()

	tests := []struct {
		name             string
		user             entities.UserIdentity
		query            map[string]string
		expectedCode     int
		expectedResponse string
		expectedErrCode  string
	}{
		{
			name:         "ok",
			user:         testutils.UserIdentities.Admin,
			expectedCode: 200,
			expectedResponse: fmt.Sprintf(`{
  "result": [
    {
      "avatar": "https://dev.null/avatars/admin.bmp",
      "background": "",
	  "id": "%s",
      "registered": true,
      "identity": {
        "id": "admin",
        "src": "iam"
      },
      "locale": "en",
      "preferredUsername": "admin",
      "publicName": "Admin",
      "username": "admin",
      "visibility": "private"
    }
  ]
}`, strconv.FormatUint(suite.users.Admin.ID, 10)),
		},
		{
			name: "query",
			user: testutils.UserIdentities.Admin,
			query: map[string]string{
				"prefix": "adm",
				"limit":  "1",
			},
			expectedCode: 200,
			expectedResponse: fmt.Sprintf(`{
  "result": [
    {
      "avatar": "https://dev.null/avatars/admin.bmp",
      "background": "",
	  "id": "%s",
	  "registered": true,
      "identity": {
        "id": "admin",
        "src": "iam"
      },
      "locale": "en",
      "preferredUsername": "admin",
      "publicName": "Admin",
      "username": "admin",
      "visibility": "private"
    }
  ]
}`, strconv.FormatUint(suite.users.Admin.ID, 10)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			httpErr := httperrors.APIError{}

			resp, err := suite.client.As(tt.user).
				SetError(&httpErr).
				SetQueryParams(tt.query).
				Get("/api/v1/orgs/yandex/users")

			yarequire.StatusCode(t, resp, err, tt.expectedCode)

			testutils.PrintResponse(resp)

			if tt.expectedResponse != "" {
				assertjson.MatchExact(t, resp.Body(), tt.expectedResponse)
			}
			if tt.expectedErrCode != "" {
				require.Equal(t, tt.expectedErrCode, httpErr.ErrorCode)
			}
		})
	}

}
