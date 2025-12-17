package integrationtests

import (
	"common/testutils/yarequire"
	"common/utils"
	"context"
	"gitcore/internal/entities"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/testutils"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/testing/protocmp"
)

func (suite *RwApiTestSuite) TestOrgService_ListUsers() {
	t := suite.T()

	ctx := testutils.AuthorizeGRPC(testutils.UserIdentities.Kopatych)
	c := pb.NewOrgServiceClient(suite.grpcClient)

	// add users to org, expecting org members: Admin, Krosh, Kopatych, Barash
	for _, user := range []*entities.User{suite.users.Krosh, suite.users.Kopatych, suite.users.Barash} {
		err := suite.MembershipRepo.Create(context.Background(), user.Identity, suite.orgs.Yandex.Identity)
		require.NoError(t, err)
	}

	resp, err := c.ListUsers(ctx, &pb.ListOrgUsersRequest{
		Id:       grpc_marshalling.IDInverse(suite.orgs.Yandex.ID),
		Query:    utils.PtrFromValue("k"),
		PageSize: utils.PtrFromValue(uint64(1)),
	})
	require.NoError(t, err)

	//yarequire.ProtoDumpFixture(t, resp)
	yarequire.ProtoCompareWithFixture(t, resp,
		protocmp.IgnoreFields(&pb.UserProfile{}, "onboarded_at", "personal_org_id", "uuid"))
}
