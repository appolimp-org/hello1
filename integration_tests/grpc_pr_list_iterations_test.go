package integrationtests

import (
	"common/cgit"
	"common/grpc"
	"common/utils"
	"common/utils/rolesgenerator/iam"
	"fmt"
	"gitcore/internal/testutils"
	"os"
	"path"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"

	"github.com/stretchr/testify/require"
)

func (suite *RwApiTestSuite) TestGrpcListPullRequestIterations() {
	t := suite.T()
	suite.addRole(t, suite.users.Kopatych, suite.repos.Alpha, iam.Roles.RepositoriesDeveloper)
	ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
	client := pb.NewPRServiceClient(suite.grpcClient)

	// create pull request

	pr := suite.makePullRequest(suite.users.Kopatych, nil)

	firstIterationID := pr.Iteration

	// initialize cgit

	slug := "yandex/alpha"

	repoURL := fmt.Sprintf("%s%s.git", suite.gitHost, slug)
	tmpDir := testutils.TempDir(t, "", "basic")
	cg := cgit.NewCGit(tmpDir).WithAuthToken(testutils.FakeIAMAuthToken(suite.users.Kopatych.Identity))
	cg.Must(t, "clone", repoURL)
	cg = cgit.NewCGit(path.Join(tmpDir, "alpha")).WithAuthToken(testutils.FakeIAMAuthToken(suite.users.Kopatych.Identity))
	cg.Must(t, "checkout", "branch")

	// create new commit

	file, err := os.Create(path.Join(tmpDir, "alpha", "newfile.txt"))
	require.NoError(t, err)

	_, err = file.WriteString("new commit\n")
	require.NoError(t, err)

	cg.Must(t, "add", ".")
	cg.Must(t, "commit", "-m", "\"add newfile.txt\"")

	// push new commit

	cg.Must(t, "push")

	// create another commit

	_, err = file.WriteString("another commit\n")
	require.NoError(t, err)

	cg.Must(t, "add", ".")
	cg.Must(t, "commit", "-m", "\"append to newfile.txt\"")

	// push again

	cg.Must(t, "push")

	// check response

	resp, err := client.ListIterations(ctx, &pb.ListIterationsRequest{
		PrId: grpc.MarshalID(pr.ID),
		Parents: &pb.RepositoryFullSlug{
			OrgSlug:  "yandex",
			RepoSlug: "alpha",
		},
	})

	require.NoError(t, err)

	require.Equal(t, 3, len(resp.Iterations))

	require.Equal(t, grpc.MarshalID(firstIterationID), resp.Iterations[0].Id)

	repoClient := pb.NewRepoServiceClient(suite.grpcClient)
	commitsList, err := repoClient.ListCommits(ctx, &pb.ListCommitsRequest{
		Id:       grpc.MarshalID(suite.repos.Alpha.ID),
		Rev:      "branch",
		PageSize: utils.PtrFromValue(uint64(3)),
	})
	require.NoError(t, err)

	for i, iteration := range resp.Iterations {
		require.Equal(t, commitsList.Commits[2-i].Hash, iteration.CommitHash)
	}
}
