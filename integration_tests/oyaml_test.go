package integrationtests

import (
	"common/oyaml"
	"common/utils"
	"context"
	except "gitcore/internal/exceptions"
	"gitcore/internal/services/config_as_code"
	"os"
	"path"
	"testing"

	"github.com/stretchr/testify/require"

	"gitcore/internal/entities"
	"gitcore/internal/testutils"
)

func (suite *RepoApiTestSuite) Test_ReadOYamlFileFromBranch() {
	t := suite.T()
	protocol := suite.HTTPSProtocol()
	_, orgSlug, repoSlug := suite.makeRandomRepo(t, suite.users.Kopatych)

	ctx := context.Background()
	repo, err := suite.RepoRepo.GetRepository(ctx, orgSlug, repoSlug)
	require.NoError(t, err)

	authenticator := suite.getFakeAuthenticator(suite.users.Kopatych.Identity)

	oy, err := suite.Caas.GetCodeReviewConfig(ctx, authenticator, entities.NewGitRevisionFromRev(repo.ID, utils.PtrFromValue("abracadabra")))
	require.ErrorIs(t, err, except.ReferenceNotFound)
	require.Nil(t, oy)

	tmpDir := testutils.TempDir(t, "", "testrepo")
	w := testutils.NewWorkdir(t, tmpDir)

	cg := protocol.PrepareCGit(w, testutils.UserIdentities.Kopatych)

	cg.Must(t, "init", ".")
	cg.Must(t, "branch", "-m", "master")
	cg.Must(t, "remote", "add", "origin", protocol.RepoURL(orgSlug, repoSlug))
	fname := "a.yaml"
	suite.makeNewFile(cg, fname)
	cg.Must(t, "add", "*")
	cg.Must(t, "commit", "-m", "cmt")
	cg.Must(t, "push", "-u", "origin", "master")

	oy, err = suite.Caas.GetCodeReviewConfig(ctx, authenticator, entities.NewGitRevisionFromRev(repo.ID, utils.PtrFromValue("master")))
	require.NoError(t, err)

	var tests = []struct {
		name      string
		yaml      string
		res       *entities.CodeReviewRules
		expectErr error
	}{
		{
			"small",
			"",
			&config_as_code.DefaultCodeReviewRules,
			nil,
		},
		{
			"pr data",
			`codereview:
  need_ships: 7
  ignore_self_ship: true
`,
			&entities.CodeReviewRules{
				NeedShips:      7,
				IgnoreSelfShip: true,
			},
			nil,
		},
		{
			"zero assigners",
			`codereview:
  need_ships: 1
  ignore_self_ship: false
  ignore_non_reviewers_block: true
  auto_assign: true
  rules:
	- patterns:
		- frontend/**
	  reviewers:
		usernames:
		assign: 0
		need_ships: 0
		ignore_self_ship: false	
			`,
			nil,
			except.ConfigAsCodeIsCorrupt,
		},
	}

	for _, tt := range tests {
		for _, configPath := range []string{oyaml.OldPath, oyaml.ReviewPath} {
			t.Run(tt.name, func(t *testing.T) {
				os.Mkdir(path.Join(cg.Path(), oyaml.SourceCraftDirectory), 0755)
				// empty old config to check newer is preferred
				os.Remove(path.Join(cg.Path(), oyaml.OldPath))
				_, err := os.Create(path.Join(cg.Path(), oyaml.OldPath))
				require.NoError(t, err)

				cp := path.Join(cg.Path(), configPath)
				nf, err := os.Create(cp)
				require.NoError(t, err)
				defer func() {
					os.Remove(cp)
				}()

				_, err = nf.WriteString(tt.yaml)
				require.NoError(t, err)

				cg.Must(t, "add", "*")
				cg.Must(t, "commit", "-m", "cmt")
				cg.Must(t, "push", "-u", "origin", "master")

				oy, err = suite.Caas.GetCodeReviewConfig(ctx, authenticator, entities.NewGitRevisionFromRev(repo.ID, utils.PtrFromValue("master")))
				if tt.expectErr != nil {
					require.ErrorIs(t, err, tt.expectErr)
				} else {
					require.NoError(t, err)
					require.Equal(t, tt.res, oy)
				}
			})
		}
	}
}
