package integrationtests

import (
	"common/cgit"
	"common/logging"
	"common/utils/rolesgenerator/iam"
	"context"
	"gitcore/internal/entities"
	"gitcore/internal/services/access"
	"gitcore/internal/testutils"
	"os"
	"path"

	"github.com/stretchr/testify/require"
)

func (suite *RwApiTestSuite) TestRepoStatisticsCalculation() {
	t := suite.T()
	ctx := context.Background()
	tmpDir := testutils.TempDir(t, "", "index")
	repoURL := suite.URL(suite.repos.Index)
	cgAdmin := cgit.NewCGit(tmpDir).WithAuthToken(testutils.StubIAMToken)
	cgAdmin.Must(t, "clone", repoURL, "index")
	dir := path.Join(tmpDir, "index")

	res, err := suite.RepoStatsRepo.Get(context.Background(), suite.repos.Index.ID)

	require.NoError(t, err)

	// список может меняться по мере того как мы донастраиваем наши эвристики
	flavors := entities.NewFlavorSet(
		"JavaScript",
		"Vim Help File",
		"Adblock Filter List",
		"XML",
		"Ecmarkup",
		"Ignore List",
		"Go",
		"HTML",
		"Text",
		"TypeScript",
	)

	require.Equal(t, flavors, res.RepoFlavors)

	kopatych, err := suite.UserRepo.GetUser(ctx, testutils.UserIdentities.Kopatych)
	require.NoError(t, err)
	err = suite.AccessBindingsService.CreateBindings(ctx, access.NullAuthenticator, []*entities.AccessBinding{
		{Subject: kopatych.Subject(), Object: suite.repos.Index.Object(), Role: iam.Roles.RepositoriesDeveloper},
	})
	require.NoError(t, err)

	cg := cgit.NewCGit(dir).
		WithAuthToken(testutils.FakeIAMAuthToken(testutils.UserIdentities.Kopatych))
	_, err = os.Create(path.Join(dir, "newlang.java"))
	require.NoError(t, err)

	logging.Info(nil, cg.Must(t, "add", "."))
	logging.Info(nil, cg.Must(t, "commit", "-m", "\"add java\""))
	logging.Info(nil, cg.Must(t, "push", "origin"))

	flavors.Add("Java")

	res, err = suite.RepoStatsRepo.Get(context.Background(), suite.repos.Index.ID)
	require.NoError(t, err)

	require.Equal(t, flavors, res.RepoFlavors)
}
