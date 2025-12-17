package integrationtests

import (
	"common/cgit"
	"common/logging"
	"common/utils"
	"common/utils/rolesgenerator/iam"
	"gitcore/internal/entities"
	"gitcore/internal/httpserver/schemas"
	"gitcore/internal/testutils"
	"github.com/stretchr/testify/require"
	"net/http"
	"os"
	"path"
	"testing"
)

func (suite *RepoApiTestSuite) TestJwtTok() {
	t := suite.T()

	owner := suite.users.Admin
	createdRepo := schemas.RepoDetails{}
	resp, err := suite.client.As(owner.Identity).
		SetBody(schemas.CreateRepositoryRequest{
			Name:       "Private",
			Slug:       "private",
			OrgSlug:    utils.PtrFromValue(suite.orgs.Yandex.Slug),
			Visibility: &entities.Visibilities.Private,
		}).
		SetResult(&createdRepo).
		Post("/api/v1/repos/")

	require.NoError(suite.T(), err)
	require.Equal(suite.T(), http.StatusCreated, resp.StatusCode())
	repoURL := suite.RepoURL(createdRepo.OrgSlug, createdRepo.Slug)

	// stub repo --->
	tmpDir := t.TempDir()
	cg := cgit.NewCGit(tmpDir)
	readme, err := os.OpenFile(path.Join(tmpDir, "readme.txt"), os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
	require.NoError(t, err)
	_, err = readme.WriteString("azazazaza\n")
	require.NoError(t, err)
	require.NoError(t, readme.Close())

	logging.Info(nil, cg.Must(t, "init", "-b", "master"))
	logging.Info(nil, cg.Must(t, "add", "."))
	logging.Info(nil, cg.Must(t, "commit", "-m", "\"add newfile.txt\""))
	cg.Must(t, "remote", "add", "origin", repoURL)

	merger := entities.UserIdentity{
		ID:               suite.cfg.ServiceAccounts.Robot.ID,
		Src:              entities.IdentityProviders.IAM,
		IsServiceAccount: true}

	ciToken, err := suite.jwtService.IssueToken(createdRepo.ID.MustToUint64(), iam.Roles.RepositoriesDeveloper, merger)
	require.NoError(t, err)
	kopatychToken, err := suite.jwtService.IssueToken(createdRepo.ID.MustToUint64(), iam.Roles.RepositoriesDeveloper, suite.users.Kopatych.Identity)
	require.NoError(t, err)

	t.Run("non-whitelisted + JWT", func(t *testing.T) {
		cg2 := cgit.NewCGit(path.Join(tmpDir)).
			WithAuthToken(testutils.FakeIAMAuthToken(merger)).
			WithJwtToken(kopatychToken)

		_, e, err := cg2.Exec("push", "--set-upstream", "origin", "master")
		require.Error(t, err)
		require.Contains(t, e, "The requested URL returned error: 403")
	})

	t.Run("plain IAM", func(t *testing.T) {
		cg2 := cgit.NewCGit(path.Join(tmpDir)).
			WithAuthToken(testutils.FakeIAMAuthToken(merger))

		_, e, err := cg2.Exec("push", "--set-upstream", "origin", "master")
		require.Error(t, err)
		require.Contains(t, e, "The requested URL returned error: 403")
	})

	t.Run("whitelisted, IAM + stolen JWT", func(t *testing.T) {
		cg2 := cgit.NewCGit(path.Join(tmpDir)).
			WithAuthToken(testutils.FakeIAMAuthToken(merger)).
			WithJwtToken(kopatychToken)

		_, e, err := cg2.Exec("push", "--set-upstream", "origin", "master")
		require.Error(t, err)
		require.Contains(t, e, "The requested URL returned error: 403")
	})

	t.Run("whitelisted, IAM + JWT", func(t *testing.T) {
		cg2 := cgit.NewCGit(path.Join(tmpDir)).
			WithAuthToken(testutils.FakeIAMAuthToken(merger)).
			WithJwtToken(ciToken)
		cg2.Must(t, "push", "--set-upstream", "origin", "master")
	})

}
