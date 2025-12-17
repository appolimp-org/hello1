package integrationtests

import (
	"common/testutils/yarequire"
	"common/utils"
	"common/utils/rolesgenerator/iam"
	"fmt"
	"gitcore/internal/httpserver/schemas"
	"gitcore/internal/testutils"
	"github.com/stretchr/testify/require"
	"net/http"
	"path"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"testing"
)

func (suite *RwApiTestSuite) TestPRGetStats() {
	t := suite.T()

	repo := suite.repos.Alpha
	author := suite.users.Kopatych
	suite.addRole(t, author, repo, iam.Roles.RepositoriesDeveloper)
	cg, tmpDir := suite.initCGit(author, repo.FullSlug())

	filePath := path.Join(tmpDir, "alpha", "newfile.go")

	commit(t, &cg, filePath, content1)
	cg.Must(t, "push")

	cg.Must(t, "checkout", "-b", "new_branch")
	commit(t, &cg, path.Join(tmpDir, "alpha", "README"), "# README\n")
	commit(t, &cg, filePath, content2)
	cg.Must(t, "push", "-u", "origin", "new_branch")

	pr := &schemas.PullRequest{}
	testutils.Expect(suite.client.As(suite.users.Kopatych.Identity).SetBody(&schemas.CreatePullRequestRequest{
		Title: "new PR", Target: "master", Source: "new_branch", Publish: true, NotificationParam: testutils.SkipNotification,
	}).SetResult(pr).Post(fmt.Sprintf("/api/v1/repos/%s/pullrequests", repo.FullSlug()))).MustBe(t, http.StatusCreated)

	iteration1 := pr.Iteration

	commit(t, &cg, filePath, content3)
	cg.Must(t, "push")

	ctx := testutils.AuthorizeGRPC(testutils.UserIdentities.Kopatych)
	c := pb.NewPRServiceClient(suite.grpcClient)

	testcases := []struct {
		name           string
		fromIter, iter *string
		expRes         *pb.GetStatsResponse
	}{
		{
			name:   "default",
			expRes: &pb.GetStatsResponse{LinesAdded: 6, LinesRemoved: 14},
		},
		{
			name:   "older iter",
			iter:   utils.PtrFromValue(string(iteration1)),
			expRes: &pb.GetStatsResponse{LinesAdded: 8, LinesRemoved: 1},
		},
		{
			name:     "between iters",
			fromIter: utils.PtrFromValue(string(iteration1)),
			expRes:   &pb.GetStatsResponse{LinesAdded: 2, LinesRemoved: 17},
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := c.GetStats(ctx, &pb.GetStatsRequest{
				PrId: string(pr.ID), IterId: tc.iter, FromIterId: tc.fromIter,
			})
			require.NoError(t, err)
			yarequire.ProtoEqual(t, tc.expRes, res)
		})
	}
}
