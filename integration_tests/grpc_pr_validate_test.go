package integrationtests

import (
	"common/grpc"
	"common/testutils/yarequire"
	"common/utils/rolesgenerator/iam"
	"gitcore/internal/entities"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/testutils"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/testing/protocmp"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"testing"
)

func (suite *RwApiTestSuite) TestGrpcValidatePullRequest() {
	t := suite.T()
	client := pb.NewPRServiceClient(suite.grpcClient)

	suite.makePullRequest(suite.users.Barash, &makePrOptions{
		Title:  "This is a very similar PR1",
		Source: "B",
		Target: "main",
		Repo:   suite.repos.MergeBase})

	suite.makePullRequest(suite.users.Barash, &makePrOptions{
		Title:  "This is a very similar PR2",
		Source: "B",
		Target: "main",
		Repo:   suite.repos.MergeBase})

	tcs := []struct {
		name    string
		code    codes.Code
		request *pb.ValidatePullRequestRequest
	}{
		{
			name: "happy path",
			request: &pb.ValidatePullRequestRequest{
				RepoId: grpc_marshalling.IDInverse(suite.repos.MergeBase.ID),
				Source: "B2",
				Target: "main",
			},
		},
		{
			name: "no src",
			request: &pb.ValidatePullRequestRequest{
				RepoId: grpc_marshalling.IDInverse(suite.repos.MergeBase.ID),
				Source: "baddd",
				Target: "main",
			},
		},
		{
			name: "no tgt",
			request: &pb.ValidatePullRequestRequest{
				RepoId: grpc_marshalling.IDInverse(suite.repos.MergeBase.ID),
				Source: "B2",
				Target: "badddd",
			},
		},
		{
			name: "similarPRs",
			request: &pb.ValidatePullRequestRequest{
				RepoId: grpc_marshalling.IDInverse(suite.repos.MergeBase.ID),
				Source: "B",
				Target: "main",
			},
		},
		{
			name: "unrelated histories",
			request: &pb.ValidatePullRequestRequest{
				RepoId: grpc_marshalling.IDInverse(suite.repos.MergeBase.ID),
				Source: "mainNonRelated",
				Target: "main",
			},
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)
			resp, err := client.Validate(ctx, tc.request)
			yarequire.ProtoStatusEqual(t, tc.code, err)
			if tc.code != codes.OK {
				return
			}

			//yarequire.ProtoDumpFixture(t, resp)
			yarequire.ProtoCompareWithFixture(t, resp,
				protocmp.IgnoreFields(&pb.PullRequestSummary{}, "id", "public_id", "created_at", "updated_at"),
			)
		})
	}
}

func (suite *RwApiTestSuite) TestPushOnOpenedPrShouldNotFail() {
	t := suite.T()
	prClient := pb.NewPRServiceClient(suite.grpcClient)

	tests := []struct {
		name     string
		user     *entities.User
		protocol Protocol
		wantErr  error
	}{
		{
			name:     "krosh commits to kopatych PR, https",
			user:     suite.users.Krosh,
			protocol: suite.HTTPSProtocol(),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			uploader := suite.users.Kopatych
			ctx := testutils.AuthorizeGRPC(tc.user.Identity)

			_, orgSlug, repoSlug := suite.makeRandomRepo(t, uploader)
			repo, err := suite.RepoRepo.GetRepository(ctx, orgSlug, repoSlug)
			require.NoError(t, err)
			suite.addRole(t, tc.user, repo, iam.Roles.RepositoriesMaintainer)

			repoURL := tc.protocol.RepoURL(orgSlug, repoSlug)
			tmpDir := testutils.TempDir(t, "", "basic")
			cg := tc.protocol.PrepareCGit(testutils.NewWorkdir(t, tmpDir), uploader.Identity)
			cg.Must(t, "init", ".")
			cg.Must(t, "remote", "add", "origin", repoURL)
			cg.Must(t, "checkout", "-b", "master")
			suite.makeNewFile(cg, "newfile1.txt")
			cg.Must(t, "add", "newfile1.txt")
			cg.Must(t, "commit", "-m", "initial")
			cg.Must(t, "push", "-u", "origin", "master")
			cg.Must(t, "checkout", "-b", "branch")
			suite.makeNewFile(cg, "newfile2.txt")
			cg.Must(t, "add", "newfile2.txt")
			cg.Must(t, "commit", "-m", "2")
			cg.Must(t, "push", "-u", "origin", "branch")

			pr := suite.makePullRequest(uploader, &makePrOptions{
				Repo:   repo,
				Title:  "TASK-1 add README.md",
				Source: "branch",
				Target: "master",
			})

			cg2 := tc.protocol.PrepareCGit(testutils.NewWorkdir(t, tmpDir), tc.user.Identity)
			suite.makeNewFile(cg2, "newfile3.txt")
			cg2.Must(t, "add", "newfile3.txt")
			cg2.Must(t, "commit", "-m", "3")
			cg2.Must(t, "push", "-u", "origin", "branch")

			prNew, err := prClient.Get(ctx, &pb.GetPullRequestRequest{
				Identity: &pb.GetPullRequestRequest_Id{
					Id: grpc.MarshalID(pr.ID),
				},
			})
			require.NoError(t, err)
			require.NotEqual(t, grpc.MarshalID(pr.Iteration), prNew.Iteration)

			feed, err := prClient.ListFeed(ctx, &pb.ListPullRequestFeedRequest{
				PrId: grpc.MarshalID(pr.ID),
			})
			require.NoError(t, err)
			require.Len(t, feed.Events, 1)

			event := feed.Events[0]
			require.Equal(t, pb.PullRequestFeedItem_ITERATION_CREATED, event.EventType)
			require.NotNil(t, event.Details.GetIteration())
			require.Equal(t, grpc.MarshalID(tc.user.ID), event.Details.GetIteration().GetUserId())
		})
	}
}
