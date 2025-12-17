package integrationtests

import (
	"common/utils"
	"common/utils/rolesgenerator/iam"
	"fmt"
	"gitcore/internal/entities"
	"gitcore/internal/httpserver/schemas"
	"gitcore/internal/testutils"
	"net/http"
	"path"

	"github.com/stretchr/testify/require"
)

func (suite *RwApiTestSuite) TestUpdatePRIteration() {
	t := suite.T()

	u := suite.users.Kopatych
	repo := suite.repos.Alpha

	suite.addRole(t, u, repo, iam.Roles.RepositoriesDeveloper)

	pr := suite.makePullRequest(u, &makePrOptions{
		Publish: utils.PtrFromValue(false),
	})

	//resolveRes := schemas.ResolveRevisionResponse{}
	//testutils.Expect(suite.client.As(u.Identity).SetResponse(&resolveRes).SetQueryParam("rev", "branch").
	//	Get(fmt.Sprintf("/api/v1/repos/%s/resolveRevision", repo.FullSlug()))).
	//	MustBe(t, http.StatusOK)

	//firstIterationID := pr.Iteration

	slug := repo.FullSlug()

	cg, tmpDir := suite.initCGit(u, slug)
	cg.Must(t, "checkout", "branch")

	filePath := path.Join(tmpDir, "alpha", "a.txt")

	commit(t, &cg, filePath, "a")
	commit(t, &cg, filePath, "b")

	cg.Must(t, "push")

	commit(t, &cg, filePath, "c")
	commit(t, &cg, filePath, "d")

	cg.Must(t, "push")

	testutils.Expect(
		suite.client.As(u.Identity).SetBody(testutils.SkipNotification).
			Post(fmt.Sprintf("/api/v1/repos/%s/%s/pullrequests/%d/%s",
				repo.OrgSlug, repo.Slug, pr.ID, schemas.PullRequestActions.Publish))).
		MustBe(t, http.StatusOK)

	commit(t, &cg, filePath, "e")
	commit(t, &cg, filePath, "f")

	cg.Must(t, "push")

	var prSchema schemas.PullRequest

	testutils.Expect(suite.client.As(u.Identity).SetResult(&prSchema).Get(
		fmt.Sprintf("/api/v1/repos/%s/%s/pullrequests/%d", repo.OrgSlug, repo.Slug, pr.ID)),
	).MustBe(t, http.StatusOK)

	require.Equal(t, entities.PRStatuses.Open, prSchema.Status)

	commitsRes := schemas.Collection[schemas.Commit]{}

	testutils.Expect(suite.client.As(u.Identity).SetResult(&commitsRes).SetQueryParam("rev", "branch").
		Get(fmt.Sprintf("/api/v1/repos/%s/commits", repo.FullSlug()))).
		MustBe(t, http.StatusOK)

	commits := commitsRes.Result[:7]

	iterations := schemas.Collection[schemas.PullRequestIteration]{}

	testutils.Expect(suite.client.As(u.Identity).SetResult(&iterations).Get(
		fmt.Sprintf("/api/v1/repos/%s/%s/pullrequests/%d/iterations", repo.OrgSlug, repo.Slug, pr.ID)),
	).MustBe(t, http.StatusOK)

	require.Equal(t, 4, len(iterations.Result))

	require.Equal(t, commits[0].Hash, iterations.Result[3].CommitHash)
	require.Equal(t, commits[2].Hash, iterations.Result[2].CommitHash)
	require.Equal(t, commits[4].Hash, iterations.Result[1].CommitHash)
	require.Equal(t, commits[6].Hash, iterations.Result[0].CommitHash)
}

func (suite *RwApiTestSuite) TestUpdatePRIterationAutoPublish() {
	t := suite.T()

	u := suite.users.Kopatych
	repo := suite.repos.Alpha

	suite.addRole(t, u, repo, iam.Roles.RepositoriesDeveloper)

	pr := suite.makePullRequest(u, &makePrOptions{
		Publish: utils.PtrFromValue(true),
	})
	require.Equal(t, entities.PRStatuses.Open, pr.Status)

	slug := repo.FullSlug()
	cg, tmpDir := suite.initCGit(u, slug)
	cg.Must(t, "checkout", "branch")

	filePath := path.Join(tmpDir, "alpha", "a.txt")

	// first push
	commit(t, &cg, filePath, "a")
	commit(t, &cg, filePath, "b")
	cg.Must(t, "push")

	var prSchema schemas.PullRequest
	testutils.Expect(suite.client.As(u.Identity).
		SetResult(&prSchema).
		Get(fmt.Sprintf("/api/v1/repos/%s/%s/pullrequests/%d", repo.OrgSlug, repo.Slug, pr.ID))).
		MustBe(t, http.StatusOK)
	require.Equal(t, entities.PRStatuses.Open, prSchema.Status)

	// second push
	commit(t, &cg, filePath, "c")
	commit(t, &cg, filePath, "d")
	cg.Must(t, "push")

	testutils.Expect(suite.client.As(u.Identity).
		SetResult(&prSchema).
		Get(fmt.Sprintf("/api/v1/repos/%s/%s/pullrequests/%d", repo.OrgSlug, repo.Slug, pr.ID))).
		MustBe(t, http.StatusOK)
	require.Equal(t, entities.PRStatuses.Open, prSchema.Status)
}
