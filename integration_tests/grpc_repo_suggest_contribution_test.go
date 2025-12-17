package integrationtests

import (
	"common/cgit"
	"common/testutils/yarequire"
	"gitcore/internal/entities"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/testutils"
	"github.com/go-git/go-git/v5/plumbing"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
)

func (suite *RwApiTestSuite) TestGrpcRepo_SuggestContribution() {
	t := suite.T()

	ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
	client := pb.NewRepoServiceClient(suite.grpcClient)

	var err error
	_, err = suite.PullRequestRepoFactory.
		Build(suite.repos.Alpha.ID).
		Create(ctx, &entities.PullRequest{
			ID:           10001,
			PublicID:     32,
			Title:        "1",
			RepoID:       suite.repos.Alpha.ID,
			SourceRepoID: suite.repos.Alpha.ID,
			AuthorID:     suite.users.Kopatych.ID,
			UpdatedBy:    suite.users.Kopatych.ID,
			Status:       entities.PRStatuses.Open,
			Settings:     *entities.NewPRSettings(),
			SourceBranch: "branch",
			TargetBranch: "master",
		}, plumbing.ZeroHash)
	require.NoError(t, err)

	_, err = suite.PullRequestRepoFactory.
		Build(suite.repos.Alpha.ID).
		Create(ctx, &entities.PullRequest{
			ID:           10002,
			PublicID:     33,
			Title:        "1 from fork",
			RepoID:       suite.repos.Alpha.ID,
			SourceRepoID: suite.repos.AlphaFork.ID,
			AuthorID:     suite.users.Kopatych.ID,
			UpdatedBy:    suite.users.Kopatych.ID,
			Status:       entities.PRStatuses.Open,
			Settings:     *entities.NewPRSettings(),
			SourceBranch: "branch",
			TargetBranch: "master",
		}, plumbing.ZeroHash)
	require.NoError(t, err)

	fork2 := suite.forkRepo(t, suite.users.Kopatych, grpc_marshalling.IDInverse(suite.repos.Alpha.ID))
	fork2ID, err := grpc_marshalling.IDDirect(fork2.Id)
	require.NoError(t, err)
	suite.addCommits(t, fork2.CloneUrl.Https, suite.users.Kopatych, commitsOptions{
		id: "main",
	})
	suite.addCommits(t, fork2.CloneUrl.Https, suite.users.Kopatych, commitsOptions{
		id: "separate",
		initializeFunc: func(t testing.TB, cg *cgit.CGit) {
			cg.Must(t, "checkout", "-b", "separate")
		},
	})
	suite.addCommits(t, fork2.CloneUrl.Https, suite.users.Kopatych, commitsOptions{
		id: "orphan",
		initializeFunc: func(t testing.TB, cg *cgit.CGit) {
			cg.Must(t, "checkout", "--orphan", "orphan")
		},
	})

	tests := map[string]struct {
		repoID       uint64
		branchName   string
		expectedCode codes.Code
		expectedPrs  []uint64
	}{
		"master": {
			repoID:       suite.repos.Alpha.ID,
			branchName:   "master",
			expectedCode: codes.OK,
		},
		"branch": {
			repoID:       suite.repos.Alpha.ID,
			branchName:   "branch",
			expectedCode: codes.OK,
		},
		"branch-not-found": {
			repoID:       suite.repos.Alpha.ID,
			branchName:   "branch2", // no such branch in AlphaFork
			expectedCode: codes.NotFound,
		},
		"fork-master": {
			repoID:       suite.repos.AlphaFork.ID,
			branchName:   "master",
			expectedCode: codes.OK,
		},
		"fork-branch": {
			repoID:       suite.repos.AlphaFork.ID,
			branchName:   "branch",
			expectedCode: codes.OK,
		},
		"fork2-ahead": {
			repoID:       fork2ID,
			branchName:   "master",
			expectedCode: codes.OK,
		},
		"fork2-separate": {
			repoID:       fork2ID,
			branchName:   "separate", // no such branch in AlphaFork
			expectedCode: codes.OK,
		},
		"orphan": {
			repoID:       fork2ID,
			branchName:   "orphan", // no such branch in AlphaFork
			expectedCode: codes.OK,
		},
	}

	for tname, test := range tests {
		t.Run(tname, func(t *testing.T) {
			resp, err := client.GetContributionSuggests(ctx, &pb.ContributionSuggestsRequest{
				Branch: &pb.BranchPointer{
					RepoId:     grpc_marshalling.IDInverse(test.repoID),
					BranchName: test.branchName,
				},
			})
			if test.expectedCode != codes.OK {
				yarequire.ProtoStatusEqual(t, test.expectedCode, err)
				return
			}
			require.NoError(t, err)

			// yarequire.ProtoDumpFixture(t, resp)
			yarequire.ProtoCompareWithFixture(t, resp)

		})
	}
}
