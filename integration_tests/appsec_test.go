package integrationtests

import (
	"context"
	"gitcore/internal/testutils"
	"testing"

	"common/testutils/yarequire"
	"common/utils/rolesgenerator/iam"
	"gitcore/internal/adapters/appsec"
	"gitcore/internal/entities"
	grpcMarshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/utils/routingscheme"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/protobuf/testing/protocmp"

	mocks "private_api/generated/mocks/appsec/v1"
	appsecPb "private_api/generated/yandex/cloud/priv/appsec/v1"
)

func (suite *RwApiTestSuite) TestAppsecService_Triggers() {
	t := suite.T()
	user := suite.users.Raichu
	repo := suite.repos.ListBranches
	suite.addRole(t, user, repo, iam.Roles.RepositoriesDeveloper)

	t.Run("push default branch", func(t *testing.T) {
		ctrl, reporter := testutils.NewMockController(t)
		defer reporter.Finish(ctrl)

		stub := mocks.NewMockScanServiceClient(ctrl)
		transportExternal := routingscheme.GetGitTransport(repo.FullSlug(), routingscheme.External, suite.cfg.Platform)

		stub.EXPECT().AppsecTrigger(gomock.Any(), yarequire.GomockProto(
			&appsecPb.AppsecTriggerRequest{
				Repository: &appsecPb.Repository{
					RepoId:           grpcMarshalling.IDInverse(repo.ID),
					OrgSlug:          repo.OrgSlug,
					RepoSlug:         repo.Slug,
					HttpsUrlExternal: transportExternal.HTTPS,
				},
				Organization: &appsecPb.OrganizationIdentity{
					Src: string(suite.orgs.Yandex.Identity.Src),
					Id:  suite.orgs.Yandex.Identity.ID,
				},
				User: &appsecPb.User{
					UserId: grpcMarshalling.IDInverse(suite.users.Admin.ID),
					Identity: &appsecPb.UserIdentity{
						Src: string(suite.users.Admin.Identity.Src),
						Id:  suite.users.Admin.Identity.ID,
					},
					Slug: suite.users.Admin.Username,
				},
				DefaultBranchName: "main",
				CommitHash:        "***",
				Ref:               "refs/heads/main",
			}, protocmp.IgnoreFields(&appsecPb.AppsecTriggerRequest{}, "commit_hash"),
		), gomock.Any()).Return(nil, nil)

		rollback := suite.Params.AppsecService.(*appsec.AppsecService).ReplaceClient(stub)
		defer rollback()

		suite.mustBash(repo, `
			git checkout main
			echo "hewwo" > my.txt
			git add .
			git commit -am "new wholesome commit full of secrets"
	`)
		suite.WaitForWorkflows(t, entities.WorkflowTypes.BroadcastRepoPushEvent)
	})

	t.Run("push other branch", func(t *testing.T) {
		ctrl, reporter := testutils.NewMockController(t)
		defer reporter.Finish(ctrl)

		stub := mocks.NewMockScanServiceClient(ctrl)
		stub.EXPECT().AppsecTrigger(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)

		rollback := suite.Params.AppsecService.(*appsec.AppsecService).ReplaceClient(stub)
		defer rollback()

		suite.mustBash(repo, `
			git checkout aBranch
			echo "hewwo" > my.txt
			git add .
			git commit -am "new wholesome commit full of secrets, but not in default branch"
	`)
		suite.WaitForWorkflows(t, entities.WorkflowTypes.BroadcastRepoPushEvent)
	})
}

func (suite *RwApiTestSuite) getAppsecEventsWith(t *testing.T, fn func()) []*appsecPb.AppsecTriggerRequest {
	ctrl, reporter := testutils.NewMockController(t)
	defer reporter.Finish(ctrl)

	var capturedRequests []*appsecPb.AppsecTriggerRequest

	stub := mocks.NewMockScanServiceClient(ctrl)
	stub.EXPECT().AppsecTrigger(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().DoAndReturn(
		func(ctx context.Context, req *appsecPb.AppsecTriggerRequest, opts ...interface{}) (*appsecPb.AppsecTriggerResponse, error) {
			capturedRequests = append(capturedRequests, req)
			return &appsecPb.AppsecTriggerResponse{}, nil
		},
	)

	rollback := suite.Params.AppsecService.(*appsec.AppsecService).ReplaceClient(stub)
	defer rollback()

	fn()

	suite.WaitForWorkflows(t, entities.WorkflowTypes.BroadcastRepoPushEvent)

	return capturedRequests
}

func (suite *RwApiTestSuite) TestAppsecService_PREvents() {
	t := suite.T()
	user := suite.users.Raichu
	repo := suite.repos.ListBranches
	suite.addRole(t, user, repo, iam.Roles.RepositoriesDeveloper)
	suite.addRole(t, suite.users.Kopatych, repo, iam.Roles.RepositoriesDeveloper)

	t.Run("pr create", func(t *testing.T) {
		events := suite.getAppsecEventsWith(t, func() {
			suite.makePullRequest(suite.users.Kopatych, &makePrOptions{
				Repo:   repo,
				Source: "aBranch",
				Target: "main",
			})
		})

		require.Len(t, events, 1)
		require.Equal(t, "main", events[0].DefaultBranchName)
		require.Equal(t, repo.OrgSlug, events[0].Repository.OrgSlug)
		require.Equal(t, repo.Slug, events[0].Repository.RepoSlug)
		require.NotEmpty(t, events[0].PrId)
	})

	t.Run("pr refresh", func(t *testing.T) {
		pr := suite.makePullRequest(suite.users.Kopatych, &makePrOptions{
			Repo:   repo,
			Source: "aBranch",
			Target: "main",
		})

		events := suite.getAppsecEventsWith(t, func() {
			suite.primitivePush(t, suite.users.Kopatych, repo, plumbing.NewBranchReferenceName("aBranch"), false)
		})

		require.NotEmpty(t, events)
		require.Equal(t, "main", events[0].DefaultBranchName)
		require.Equal(t, repo.OrgSlug, events[0].Repository.OrgSlug)
		require.Equal(t, repo.Slug, events[0].Repository.RepoSlug)
		require.NotEmpty(t, events[0].PrId)

		_ = pr
	})
}
