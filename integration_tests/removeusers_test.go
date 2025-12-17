package integrationtests

import (
	"common/testutils/assertjson"
	"common/utils"
	"common/utils/rolesgenerator/iam"
	"context"
	"gitcore/internal/entities"
	"gitcore/internal/httpserver/httperrors"
	"gitcore/internal/testutils"
	"testing"

	"github.com/stretchr/testify/require"
)

func (suite *RepoApiTestSuite) TestOrgs_RemoveUsers() {
	t := suite.T()

	httpErr := httperrors.APIError{}

	// make him admin
	suite.addOrgRole(t, suite.users.Slowpoke, suite.orgs.Yandex, iam.Roles.OrganizationManagerAdmin)

	// Add some users

	for _, user := range []entities.UserIdentity{testutils.UserIdentities.Barash, testutils.UserIdentities.Krosh} {
		err := suite.MembershipRepo.Create(context.Background(), user, suite.orgs.Yandex.Identity)
		require.NoError(t, err)
	}

	tests := []struct {
		name             string
		user             entities.UserIdentity
		expectedCode     int
		expectedResponse *string
		expectedErrCode  string
	}{
		{
			name:             "remove Krosh (ok)",
			user:             testutils.UserIdentities.Krosh,
			expectedCode:     204,
			expectedResponse: utils.PtrFromValue(""),
		},
		{
			name:             "remove Kopatych (not found)",
			user:             testutils.UserIdentities.Kopatych,
			expectedCode:     204,
			expectedResponse: utils.PtrFromValue(""),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := suite.client.As(testutils.UserIdentities.Slowpoke).
				SetError(&httpErr).
				SetBody(&tt.user).
				Post("/api/v1/orgs/yandex/users/removeByIdentity")

			require.NoError(t, err)
			require.Equal(t, tt.expectedCode, resp.StatusCode(), string(resp.Body()))
			if tt.expectedResponse != nil {
				assertjson.MatchExact(t, resp.Body(), *tt.expectedResponse)
			}
			if tt.expectedErrCode != "" {
				require.Equal(t, tt.expectedErrCode, httpErr.ErrorCode)
			}
		})

	}
}
