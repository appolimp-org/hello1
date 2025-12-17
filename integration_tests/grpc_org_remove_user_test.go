package integrationtests

import (
	"common/grpc"
	"common/testutils/yarequire"
	"context"
	"gitcore/internal/entities"
	"gitcore/internal/testutils"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
)

func (suite *RwApiTestSuite) TestGrpcOrg_RemoveUser() {
	t := suite.T()

	// Add some users
	for _, user := range []entities.UserIdentity{testutils.UserIdentities.Barash, testutils.UserIdentities.Krosh} {
		err := suite.MembershipRepo.Create(context.Background(), user, suite.orgs.Yandex.Identity)
		require.NoError(t, err)
	}

	client := pb.NewOrgServiceClient(suite.grpcClient)

	tests := []struct {
		name         string
		user         *entities.User
		caller       *entities.User
		expectedCode codes.Code
	}{
		{
			name:         "remove Krosh - ok",
			user:         suite.users.Krosh,
			caller:       suite.users.Admin,
			expectedCode: codes.OK,
		},
		{
			name:         "remove Barash - permission denied",
			user:         suite.users.Barash,
			caller:       suite.users.Kopatych,
			expectedCode: codes.PermissionDenied,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.RemoveUser(testutils.AuthorizeGRPC(tt.caller.Identity),
				&pb.RemoveUserRequest{
					Id:     grpc.MarshalID(suite.orgs.Yandex.ID),
					UserId: grpc.MarshalID(tt.user.ID),
				})
			yarequire.ProtoStatusEqual(t, tt.expectedCode, err)
		})
	}
}
