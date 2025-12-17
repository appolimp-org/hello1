package integrationtests

import (
	"common/testutils/yarequire"
	"fmt"
	"gitcore/internal/entities"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/testutils"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"slices"
	"testing"
)

func (suite *RwApiTestSuite) TestGrpcRepoGetAvailableActions() {
	t := suite.T()
	client := pb.NewRepoServiceClient(suite.grpcClient)

	noPerm := map[string][]*entities.User{
		suite.repos.AuthRepoPrivate.Slug:  {suite.users.AuthNobody, suite.users.AuthMember},
		suite.repos.AuthRepoInternal.Slug: {suite.users.AuthNobody},
	}

	for _, repo := range suite.AllAuthRepos {
		for _, user := range suite.AuthMatrixUsers {
			t.Run(fmt.Sprintf("repo %s %s", repo.Visibility, user.Username), func(t *testing.T) {
				ctx := testutils.AuthorizeGRPC(user.Identity)
				resp, err := client.GetAvailableActions(ctx, &pb.GetAvailableRepoActionsRequest{Id: grpc_marshalling.IDInverse(repo.ID)})

				code := status.Convert(err)
				if code.Code() == codes.PermissionDenied {
					if !slices.Contains(noPerm[repo.Slug], user) {
						t.Error("got Forbidden while should get OK")
					}
					return
				}

				yarequire.ProtoStatusEqual(t, codes.OK, err)
				//if code != codes.OK {
				//	return
				//}

				//yarequire.ProtoDumpFixture(t, resp)
				yarequire.ProtoCompareWithFixture(t, resp)
			})
		}
	}
}
