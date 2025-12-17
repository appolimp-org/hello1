package integrationtests

import (
	"common/testutils/yarequire"
	"common/utils/rolesgenerator/iam"
	"gitcore/internal/entities"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/testutils"
	"google.golang.org/grpc/codes"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"testing"
)

func (suite *RwApiTestSuite) TestGrpcOrgGetAvailableActions() {
	t := suite.T()
	client := pb.NewOrgServiceClient(suite.grpcClient)

	memberFederal := suite.UserFixture(entities.UserIdentity{
		ID:  "federal1",
		Src: entities.IdentityProviders.IAM,
	}, entities.Visibilities.Private)

	suite.addOrgRole(t, memberFederal, suite.orgs.Smeshariki, iam.Roles.InternalOrganizationManagerMember)
	suite.addOrgRole(t, suite.users.Krosh, suite.orgs.Smeshariki, iam.Roles.InternalOrganizationManagerMember)

	notMemberFederal := suite.UserFixture(entities.UserIdentity{
		ID:  "federal2",
		Src: entities.IdentityProviders.IAM,
	}, entities.Visibilities.Private)

	tcs := []struct {
		name  string
		orgID uint64
		user  entities.UserIdentity
		code  codes.Code
	}{
		{
			name:  "owner",
			orgID: suite.orgs.Yandex.ID,
			user:  suite.users.Admin.Identity,
		},
		{
			name:  "member",
			orgID: suite.orgs.Smeshariki.ID,
			user:  suite.users.Krosh.Identity,
		},
		{
			name:  "non-member",
			orgID: suite.orgs.Smeshariki.ID,
			user:  suite.users.Pikachu.Identity,
		},
		{
			name:  "member-federal",
			orgID: suite.orgs.Smeshariki.ID,
			user:  memberFederal.Identity,
		},
		{
			name:  "non-member-federal",
			orgID: suite.orgs.Smeshariki.ID,
			user:  notMemberFederal.Identity,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			ctx := testutils.AuthorizeGRPC(tc.user)
			resp, err := client.GetAvailableActions(ctx, &pb.GetAvailableOrgActionsRequest{Id: grpc_marshalling.IDInverse(tc.orgID)})
			yarequire.ProtoStatusEqual(t, tc.code, err)
			if tc.code != codes.OK {
				return
			}

			//yarequire.ProtoDumpFixture(t, resp)
			yarequire.ProtoCompareWithFixture(t, resp)
		})
	}
}
