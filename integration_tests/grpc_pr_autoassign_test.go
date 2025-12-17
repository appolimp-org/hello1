package integrationtests

import (
	"fmt"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"path"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"slices"

	"github.com/stretchr/testify/require"

	"common/functools"
	"common/oyaml"
	"common/utils/rolesgenerator/iam"
	"gitcore/internal/testutils"
)

func (suite *RwApiTestSuite) TestPRAutoAssignSideEffect() {
	for _, configPath := range []string{oyaml.ReviewPath, oyaml.OldPath} {
		suite.BeforeTest("", "")

		t := suite.T()

		repo := suite.repos.TreeDiff
		author := suite.users.Kopatych
		suite.addRole(t, author, repo, iam.Roles.RepositoriesDeveloper)

		cg, tmpDir := suite.initCGit(author, repo.FullSlug())
		cg.Must(t, "checkout", "main")
		// empty old config to check newer is preferred
		commit(t, &cg, path.Join(tmpDir, repo.Slug, oyaml.OldPath), "")
		commit(t, &cg, path.Join(tmpDir, repo.Slug, configPath), fmt.Sprintf(`
codereview:
  need_ships: 1
  auto_assign: true
  rules:
    - patterns:
        - '**'
      reviewers:
        usernames:
          - "%s"
          - "%s"
        assign: 2
    - patterns:
        - "**"
      reviewers:
        usernames:
          - "%s"
          - "%s"
        assign: 2
`, suite.users.Krosh.Username, suite.users.Barash.Username, suite.users.Barash.Username, suite.users.Pikachu.Username))
		cg.Must(t, "push")

		pr := suite.makePullRequest(author, &makePrOptions{
			Repo:   repo,
			Title:  "PR with rules",
			Target: "main",
			Source: "branch",
		})

		client := pb.NewPRReviewersServiceClient(suite.grpcClient)
		reviewers, err := client.List(testutils.AuthorizeGRPC(author.Identity), &pb.ListReviewersRequest{
			PrId: grpc_marshalling.IDInverse(pr.ID),
		})
		require.NoError(t, err)
		require.Len(t, reviewers.Reviewers, 3)

		ids := functools.Map(reviewers.Reviewers, (*pb.PRReviewer).GetUserId)
		slices.Sort(ids)

		require.Contains(t, ids, grpc_marshalling.IDInverse(suite.users.Barash.ID))
		require.Contains(t, ids, grpc_marshalling.IDInverse(suite.users.Krosh.ID))
		require.Contains(t, ids, grpc_marshalling.IDInverse(suite.users.Pikachu.ID))

		suite.AfterTest("", "")
	}
}
