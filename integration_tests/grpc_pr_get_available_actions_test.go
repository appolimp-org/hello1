package integrationtests

import (
	"common/testutils/yarequire"
	"common/utils/rolesgenerator/iam"
	"fmt"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/testutils"
	"github.com/stretchr/testify/require"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"testing"
)

func (suite *RwApiTestSuite) TestGrpcPrGetAvailableActions() {
	t := suite.T()

	repoClient := pb.NewRepoServiceClient(suite.grpcClient)
	client := pb.NewPRServiceClient(suite.grpcClient)
	repo := suite.repos.AuthRepoPublic
	suite.addRole(t, suite.users.Kopatych, repo, iam.Roles.RepositoriesAdmin)

	for _, user := range suite.AuthMatrixUsers {
		t.Run(fmt.Sprintf("%s as pr author", user.Username), func(t *testing.T) {
			ctx := testutils.AuthorizeGRPC(user.Identity)

			repoActions, err := repoClient.GetAvailableActions(ctx, &pb.GetAvailableRepoActionsRequest{Id: grpc_marshalling.IDInverse(repo.ID)})
			require.NoError(t, err)
			if repoActions.CreatePrs {
				t.Logf("user %v can't create PRs, no test to perform", user.Identity)
				return
			}

			pr := suite.makePullRequest(user, &makePrOptions{
				Repo:   repo,
				Source: "branch",
				Target: "master",
			})
			resp, err := client.GetAvailableActions(ctx, &pb.GetAvailablePRActionsRequest{Id: grpc_marshalling.IDInverse(pr.ID)})
			require.NoError(t, err)

			yarequire.ProtoEqual(t, &pb.GetAvailablePRActionsResponse{
				Merge:               true,
				ChangeStatus:        true,
				Edit:                true,
				ModifyReviewers:     true,
				LeaveReviewDecision: true,
				Comment:             true,
				PublishAllComments:  true,
				EditOwnComment:      true,
				EditAnyComment:      true,
				NeuroReview:         true,
			}, resp)
		})

		t.Run(user.Username, func(t *testing.T) {
			pr := suite.makePullRequest(suite.users.Kopatych, &makePrOptions{
				Repo:   repo,
				Source: "branch",
				Target: "master",
			})

			ctx := testutils.AuthorizeGRPC(user.Identity)
			resp, err := client.GetAvailableActions(ctx, &pb.GetAvailablePRActionsRequest{Id: grpc_marshalling.IDInverse(pr.ID)})
			require.NoError(t, err)

			//yarequire.ProtoDumpFixture(t, resp)
			yarequire.ProtoCompareWithFixture(t, resp)
		})
	}
}
