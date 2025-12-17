package integrationtests

import (
	"common/cgit"
	"common/functools"
	"common/logging"
	"context"
	"fmt"
	"gitcore/internal/entities"
	"gitcore/internal/testutils"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/stretchr/testify/require"
	"os"
	"path"
	"testing"
)

func (suite *RwApiTestSuite) TestCloneDefaultBranch() {
	type testcase struct {
		defaultBranch string
		repo          *entities.Repository
		requireFiles  []string
	}

	tests := []testcase{
		{
			defaultBranch: "master",
			repo:          suite.repos.Blame,
			requireFiles:  []string{"file-rename.txt", "file3.txt"},
		},
		{
			defaultBranch: "main",
			repo:          suite.repos.PrValidation,
			requireFiles:  []string{"file1.txt"},
		},
	}

	for _, test := range tests {
		name := fmt.Sprintf("clone wtih %s", test.defaultBranch)
		suite.T().Run(name, func(t *testing.T) {
			tmpDir := testutils.TempDir(t, "", "basic")
			repoURL := suite.URL(test.repo)

			cgAdmin := cgit.NewCGit(tmpDir).WithAuthToken(testutils.StubIAMToken)
			cgAdmin.Must(t, "clone", repoURL, test.repo.Name)

			entries, err := os.ReadDir(path.Join(tmpDir, test.repo.Name))
			require.NoError(t, err)
			entriesSet := functools.SliceToSetF(entries, func(entry os.DirEntry) string {
				return entry.Name()
			})

			require.Contains(t, entriesSet, ".git")
			for _, file := range test.requireFiles {
				require.Contains(t, entriesSet, file)
			}
		})
	}
}

func (suite *RwApiTestSuite) TestPushAttributes() {
	t := suite.T()
	tmpDir := testutils.TempDir(t, "", "basic")
	repoURL := suite.URL(suite.repos.Alpha)
	cgAdmin := cgit.NewCGit(tmpDir).WithAuthToken(testutils.StubIAMToken)
	cgAdmin.Must(t, "clone", repoURL, "alpha")
	alphaDir := path.Join(tmpDir, "alpha")

	t.Run("push", func(t *testing.T) {
		ctx := context.Background()
		user, err := suite.UserRepo.GetUser(ctx, testutils.UserIdentities.Admin)
		require.NoError(t, err)

		cg := cgit.NewCGit(alphaDir).
			WithAuthToken(testutils.FakeIAMAuthToken(user.Identity))
		attrContent := "* text=auto"
		require.NoError(t, os.WriteFile(path.Join(alphaDir, ".gitattributes"), []byte(attrContent), 0777))
		require.NoError(t, os.Mkdir(path.Join(alphaDir, "subdir"), 0777))
		require.NoError(t, os.WriteFile(path.Join(alphaDir, "subdir", ".gitattributes"), []byte("ignored"), 0777))
		require.NoError(t, os.WriteFile(path.Join(alphaDir, "subdir", "sameasgitattr.txt"), []byte(attrContent), 0777))

		logging.Info(nil, cg.Must(t, "add", "."))
		logging.Info(nil, cg.Must(t, "commit", "-m", "\"add .gitattributes\""))
		_, _, err = cg.Exec("push", "origin")
		require.NoError(t, err)

		gitFSLoader := suite.GitFSFactory.Build(suite.repos.Alpha.ID)
		gitFS, _, err := gitFSLoader.Load(context.Background())
		require.NoError(t, err)

		objs, err := gitFS.GetObjectsByHash(ctx, []plumbing.Hash{
			plumbing.NewHash("2125666142eb661091cc5f32af2ed2f3fc65571d"),
		})
		require.NoError(t, err)
		require.Equal(t, 1, len(objs))
		content, err := gitFS.ReadFile(ctx, objs[0])
		require.NoError(t, err)
		require.Equal(t, attrContent, string(content))
	})

}
