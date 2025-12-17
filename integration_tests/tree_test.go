package integrationtests

import (
	"context"
	"gitcore/internal/di"
	"gitcore/internal/interfaces"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"
	"testing"
)

var commitHashHead = plumbing.NewHash("5a5bf59a3c99e97462daa9dc7bf4dbd40545fdb6")

func TestGetEntry(t *testing.T) {
	ctx := context.Background()

	var metadataRepoFactory interfaces.MetadataRepositoryFactory

	dc := di.WireUp(t, fx.Populate(&metadataRepoFactory))
	dc.World.MakeOrgs(t)
	repoID, _ := dc.World.ImportFixture(t, "generated/treemanipulations")

	metadataRepo := metadataRepoFactory.Build(repoID)

	c2, err := metadataRepo.GetCommitByHash(ctx, commitHashHead)
	require.NoError(t, err)

	gitFS, closer, err := dc.GitFSFactory.Build(repoID).Load(ctx)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, closer())
	}()

	t.Run("find file entry", func(t *testing.T) {
		path := "foo/bar/vendor/a/b/c/d/zzzz.txt"
		entry, err := gitFS.FindTreeEntry(ctx, c2.TreeHash, path)
		require.NoError(t, err)
		require.Equal(t, "zzzz.txt", entry.Name)
	})

	t.Run("find dir entry", func(t *testing.T) {
		path := "foo/bar/vendor/a/b/c/d"
		entry, err := gitFS.FindTreeEntry(ctx, c2.TreeHash, path)
		require.NoError(t, err)
		require.Equal(t, "d", entry.Name)
		require.Equal(t, filemode.Dir, entry.Mode)
	})
}
