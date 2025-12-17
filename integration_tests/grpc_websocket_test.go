package integrationtests

import (
	"common/grpc"
	"common/testutils/yarequire"
	"common/utils"
	"common/utils/rolesgenerator/iam"
	"context"
	"gitcore/internal/adapters/centrifugo"
	"gitcore/internal/entities"
	"gitcore/internal/httpserver/schemas"
	"gitcore/internal/testutils"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"net/http"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"testing"
)

func (suite *RwApiTestSuite) TestWebsocketHandler_GenerateAuthToken() {
	t := suite.T()

	// create private org
	repoSlug := uuid.NewString()
	testutils.Expect(suite.client.As(testutils.UserIdentities.Admin).
		SetBody(schemas.CreateRepositoryRequest{
			Name:       uuid.NewString(),
			Slug:       repoSlug,
			OrgSlug:    utils.PtrFromValue(suite.orgs.Yandex.Slug),
			Visibility: &entities.Visibilities.Private,
		}).
		Post("/api/v1/repos/")).
		MustBe(t, http.StatusCreated)

	privateOrg, err := suite.RepoRepo.GetRepository(context.Background(), suite.orgs.Yandex.Slug, repoSlug)
	require.NoError(t, err)

	issue1 := suite.makeIssue(suite.users.Kopatych, &makeIssueOptions{
		RepoID:     suite.repos.Alpha.ID,
		Title:      "The Issue",
		Visibility: entities.IssueVisibilities.Public,
	})
	issue2 := suite.makeIssue(suite.users.Krosh, &makeIssueOptions{
		RepoID:     suite.repos.Alpha.ID,
		Title:      "The Issue",
		Visibility: entities.IssueVisibilities.Private,
	})

	pr := suite.makePullRequest(suite.users.Krosh, &makePrOptions{
		Repo:      suite.repos.Alpha,
		Title:     "1 - alpha",
		Reviewers: []uint64{suite.users.PinPublic.ID, suite.users.Slowpoke.ID},
	})

	tests := []struct {
		name         string
		user         *entities.User
		reposViewer  []*entities.Repository
		request      *pb.GenerateAuthTokenRequest
		wantResponse *pb.GenerateAuthTokenResponse
		wantCode     codes.Code
	}{
		{
			name: "ok",
			user: suite.RandomUserFixture(),
			reposViewer: []*entities.Repository{
				suite.repos.Alpha,
				suite.repos.History,
				privateOrg,
			},
			request: &pb.GenerateAuthTokenRequest{
				AutoSubscribeObjects: []*pb.ObjectIdentity{
					{
						Type: pb.ObjectIdentity_REPOSITORY,
						Id:   grpc.MarshalID(suite.repos.Alpha.ID),
					},
					{
						Type: pb.ObjectIdentity_ISSUE,
						Id:   grpc.MarshalID(issue1.ID),
					},
					{
						Type: pb.ObjectIdentity_PULL_REQUEST,
						Id:   grpc.MarshalID(pr.ID),
					},
				},
			},
			wantResponse: &pb.GenerateAuthTokenResponse{
				Token: centrifugo.StubAuthToken,
			},
			wantCode: codes.OK,
		},
		{
			name: "public_repos",
			user: suite.RandomUserFixture(),
			request: &pb.GenerateAuthTokenRequest{
				AutoSubscribeObjects: []*pb.ObjectIdentity{
					{
						Type: pb.ObjectIdentity_REPOSITORY,
						Id:   grpc.MarshalID(suite.repos.Alpha.ID),
					},
					{
						Type: pb.ObjectIdentity_REPOSITORY,
						Id:   grpc.MarshalID(suite.repos.TreeDiff.ID),
					},
				},
			},
			wantResponse: &pb.GenerateAuthTokenResponse{
				Token: centrifugo.StubAuthToken,
			},
			wantCode: codes.OK,
		},
		{
			name: "no_access",
			user: suite.RandomUserFixture(),
			request: &pb.GenerateAuthTokenRequest{
				AutoSubscribeObjects: []*pb.ObjectIdentity{
					{
						Type: pb.ObjectIdentity_REPOSITORY,
						Id:   grpc.MarshalID(privateOrg.ID),
					},
				},
			},
			wantCode: codes.PermissionDenied,
		},
		{
			name: "no_access-issue",
			user: suite.RandomUserFixture(),
			request: &pb.GenerateAuthTokenRequest{
				AutoSubscribeObjects: []*pb.ObjectIdentity{
					{
						Type: pb.ObjectIdentity_ISSUE,
						Id:   grpc.MarshalID(issue2.ID),
					},
				},
			},
			wantCode: codes.PermissionDenied,
		},
		{
			name: "no_repo",
			user: suite.RandomUserFixture(),
			request: &pb.GenerateAuthTokenRequest{
				AutoSubscribeObjects: []*pb.ObjectIdentity{
					{
						Type: pb.ObjectIdentity_ORGANIZATION,
						Id:   "some_id",
					},
				},
			},
			wantCode: codes.InvalidArgument,
		},
		{
			name: "mixed-objects",
			user: suite.RandomUserFixture(),
			reposViewer: []*entities.Repository{
				suite.repos.Alpha,
				suite.repos.History,
				privateOrg,
			},
			request: &pb.GenerateAuthTokenRequest{
				AutoSubscribeObjects: []*pb.ObjectIdentity{
					{
						Type: pb.ObjectIdentity_REPOSITORY,
						Id:   grpc.MarshalID(suite.repos.Alpha.ID),
					},
					{
						Type: pb.ObjectIdentity_REPOSITORY,
						Id:   grpc.MarshalID(suite.repos.TreeDiff.ID),
					},
					{
						Type: pb.ObjectIdentity_REPOSITORY,
						Id:   grpc.MarshalID(privateOrg.ID),
					},
				},
			},
			wantResponse: &pb.GenerateAuthTokenResponse{
				Token: centrifugo.StubAuthToken,
			},
			wantCode: codes.OK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, repo := range tt.reposViewer {
				suite.addRole(t, tt.user, repo, iam.Roles.RepositoriesViewer)
			}

			ctx := testutils.AuthorizeGRPC(tt.user.Identity)
			client := pb.NewWebSocketServiceClient(suite.grpcClient)

			resp, err := client.GenerateAuthToken(ctx, tt.request)
			yarequire.ProtoStatusEqual(t, tt.wantCode, err)
			yarequire.ProtoEqual(t, tt.wantResponse, resp)
		})
	}
}
