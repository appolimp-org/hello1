package integrationtests

import (
	"common/functools"
	"common/testutils/yarequire"
	"common/utils"
	"common/utils/rolesgenerator/iam"
	"gitcore/internal/entities"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/testutils"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (suite *RwApiTestSuite) TestGrpcSuggestPullRequest() {
	t := suite.T()
	client := pb.NewRepoServiceClient(suite.grpcClient)

	// create repo
	ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
	res, err := client.Create(ctx, &pb.CreateRepositoryRequest{
		Org: &pb.CreateRepositoryRequest_OrgId{
			OrgId: grpc_marshalling.IDInverse(suite.orgs.Smeshariki.ID),
		},
		Slug:          "repo1",
		Visibility:    pb.ResourceVisibility_RESOURCE_PUBLIC,
		DefaultBranch: "master",
	})
	require.NoError(t, err)

	repoPb, err := grpc_marshalling.OperationResponse(res, &pb.Repository{})
	require.NoError(t, err)
	repoID, err := grpc_marshalling.IDDirect(repoPb.Id)
	require.NoError(t, err)
	repo, err := suite.RepoRepo.GetRepositoryByID(ctx, repoID)
	require.NoError(t, err)

	protocol := suite.HTTPSProtocol()
	repoURL := protocol.RepoURL(repo.OrgSlug, repo.Slug)

	now := time.Now().UTC()

	// push from Admin
	suite.addRole(t, suite.users.Admin, repo, iam.Roles.RepositoriesAdmin)
	suite.makeCommit(t, protocol, repoURL, commitArgs{
		user:       suite.users.Admin,
		branch:     "master",
		commitTime: now.Add(-2 * time.Hour),
	})

	// push from Kopatych
	suite.addRole(t, suite.users.Kopatych, repo, iam.Roles.RepositoriesMaintainer)
	suite.makeCommit(t, protocol, repoURL, commitArgs{
		user:         suite.users.Kopatych,
		branch:       "b1",
		tag:          "t1",
		parentBranch: "master",
		commitTime:   now.Add(-2 * time.Hour),
	})
	suite.makeCommit(t, protocol, repoURL, commitArgs{
		user:       suite.users.Kopatych,
		branch:     "b2",
		commitTime: now.Add(-4 * time.Hour),
	})
	suite.makePullRequest(suite.users.Kopatych, &makePrOptions{
		Repo:    repo,
		Source:  "b1",
		Target:  "master",
		Publish: utils.PtrFromValue(true),
	})

	// push from Krosh
	suite.addRole(t, suite.users.Krosh, repo, iam.Roles.RepositoriesMaintainer)
	suite.makeCommit(t, protocol, repoURL, commitArgs{
		user:       suite.users.Krosh,
		branch:     "b3",
		tag:        "t2",
		commitTime: now.Add(-2 * time.Hour),
	})
	suite.makeCommit(t, protocol, repoURL, commitArgs{
		user:         suite.users.Krosh,
		branch:       "b4",
		parentBranch: "master",
		commitTime:   now.Add(-1 * time.Hour),
	})
	suite.makePullRequest(suite.users.Krosh, &makePrOptions{
		Repo:    repo,
		Source:  "b4",
		Target:  "master",
		Publish: utils.PtrFromValue(true),
	})

	// push from Barash
	suite.addRole(t, suite.users.Barash, repo, iam.Roles.RepositoriesMaintainer)
	suite.makeCommit(t, protocol, repoURL, commitArgs{
		user:       suite.users.Barash,
		branch:     "b5",
		commitTime: now.Add(-2 * time.Hour),
	})
	suite.makeCommit(t, protocol, repoURL, commitArgs{
		user:       suite.users.Barash,
		branch:     "b6",
		commitTime: now.Add(-1 * time.Hour),
	})

	// push from Raichu
	suite.addRole(t, suite.users.Raichu, repo, iam.Roles.RepositoriesMaintainer)
	suite.makeCommit(t, protocol, repoURL, commitArgs{
		user:         suite.users.Raichu,
		branch:       "b7",
		parentBranch: "master",
	})
	suite.makeCommit(t, protocol, repoURL, commitArgs{
		user:         suite.users.Raichu,
		branch:       "b8",
		parentBranch: "master",
	})

	// create and merge pr
	pr := suite.makePullRequest(suite.users.Barash, &makePrOptions{
		Repo:    repo,
		Source:  "b7",
		Target:  "master",
		Publish: utils.PtrFromValue(true),
	})
	_, err = suite.PullRequestService.Merge(
		ctx,
		pr,
		repo.OrgID,
		suite.users.Barash,
		suite.getFakeAuthenticator(suite.users.Barash.Identity),
		true,
		entities.NotifyOptions{},
	)
	require.NoError(t, err)
	suite.WaitForWorkflows(t, entities.WorkflowTypes.MergeV2PullRequest)

	// create pr, merge and push again
	pr = suite.makePullRequest(suite.users.Raichu, &makePrOptions{
		Repo:    repo,
		Source:  "b8",
		Target:  "master",
		Publish: utils.PtrFromValue(true),
	})
	_, err = suite.PullRequestService.Merge(
		ctx,
		pr,
		repo.OrgID,
		suite.users.Raichu,
		suite.getFakeAuthenticator(suite.users.Raichu.Identity),
		true,
		entities.NotifyOptions{},
	)
	require.NoError(t, err)
	suite.WaitForWorkflows(t, entities.WorkflowTypes.MergeV2PullRequest)
	suite.makeCommit(t, protocol, repoURL, commitArgs{
		user:         suite.users.Raichu,
		branch:       "b8",
		parentBranch: "master",
	})

	tt := map[string]struct {
		user             *entities.User
		expectedBranches []string
	}{
		"Newer branch with pr, older before bound - nothing to suggest": {
			user:             suite.users.Kopatych,
			expectedBranches: []string{},
		},
		"Newer branch with pr, older after bound - suggest older": {
			user:             suite.users.Krosh,
			expectedBranches: []string{"b3"},
		},
		"Two branches after bound - suggest newer": {
			user:             suite.users.Barash,
			expectedBranches: []string{"b6", "b5"},
		},
		"Merged branch should not be suggested, but with new commits should be": {
			user:             suite.users.Raichu,
			expectedBranches: []string{"b8"},
		},
		"No branches - nothing to suggest": {
			user:             suite.users.Slowpoke,
			expectedBranches: []string{},
		},
		"Default branch should not be suggested": {
			user:             suite.users.Admin,
			expectedBranches: []string{},
		},
	}

	for tn, tc := range tt {
		t.Run(tn, func(t *testing.T) {
			ctx := testutils.AuthorizeGRPC(tc.user.Identity)
			resp, err := client.SuggestPullRequest(ctx, &pb.SuggestPullRequestRequest{
				Id: grpc_marshalling.IDInverse(repo.ID),
			})
			yarequire.ProtoStatusEqual(t, codes.OK, err)
			require.EqualValues(t, tc.expectedBranches, functools.Map(resp.Branches, func(b *pb.Branch) string {
				return b.Name
			}))
		})
	}
}

type commitArgs struct {
	user         *entities.User
	branch       string
	tag          string
	parentBranch string
	commitTime   time.Time
}

func (suite *RwApiTestSuite) makeCommit(t *testing.T, protocol Protocol, repoURL string, args commitArgs) {
	tmpDir := testutils.TempDir(t, "", "testrepo")
	w := testutils.NewWorkdir(t, tmpDir)

	cg := protocol.PrepareCGit(w, args.user.Identity)
	cg.Must(t, "init", ".")
	cg.Must(t, "remote", "add", "origin", repoURL)

	if args.parentBranch != "" {
		cg.Must(t, "pull", "origin", args.parentBranch)
		cg.Must(t, "checkout", args.parentBranch)
	}

	cg.Must(t, "checkout", "-b", args.branch)

	suite.makeNewFileWithContent(cg, uuid.NewString(), `some content`)

	require.NoError(t, cg.SetUserName(args.user.Username))
	require.NoError(t, cg.SetEmail(args.user.Email))
	cg = cg.WithCommitTime(args.commitTime)
	suite.commitAll(cg, "add config", args.branch)

	if args.tag != "" {
		cg.Must(t, "tag", args.tag)
		cg.Must(t, "push", "origin", "--tag")
	}
}

func (suite *RwApiTestSuite) TestGrpcSuggestPullRequest_EmptyEmail() {
	t := suite.T()

	// create user with empty email
	userIdentity := entities.UserIdentity{
		ID:  "sovunya",
		Src: entities.IdentityProviders.IAM,
	}
	suite.StubClaimService.SetClaimsForUnittestUser(userIdentity, entities.Claims{
		PublicName:        "Sovunya",
		PreferredUsername: "sovunya",
		Email:             "",
	})
	ctx := testutils.AuthorizeGRPC(userIdentity)

	meClient := pb.NewMeServiceClient(suite.grpcClient)

	_, err := meClient.GetProfile(ctx, &pb.GetMyProfileRequest{})
	require.NoError(t, err)

	repoClient := pb.NewRepoServiceClient(suite.grpcClient)

	// suggest pr - should fail
	_, err = repoClient.SuggestPullRequest(ctx, &pb.SuggestPullRequestRequest{
		Id: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
	})
	yarequire.ProtoStatusEqual(t, codes.FailedPrecondition, err)
	require.Equal(
		t,
		"user <iam:sovunya> has empty email; can't match commits",
		status.Convert(err).Message(),
	)

	// set email in cloud
	suite.StubClaimService.SetClaimsForUnittestUser(userIdentity, entities.Claims{
		PublicName:        "Sovunya",
		PreferredUsername: "sovunya",
		Email:             "sovunya@ya.ru",
	})

	// suggest works
	_, err = repoClient.SuggestPullRequest(ctx, &pb.SuggestPullRequestRequest{
		Id: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
	})
	require.NoError(t, err)
}
