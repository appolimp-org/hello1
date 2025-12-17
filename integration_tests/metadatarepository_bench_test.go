package integrationtests

import (
	"common/functools"
	"context"
	"gitcore/internal/di"
	"gitcore/internal/entities"
	"gitcore/internal/interfaces"
	"gitcore/internal/utils"
	"go.uber.org/fx"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/stretchr/testify/require"
	"golang.org/x/exp/rand"
)

func BenchmarkGetCommitsByHash(b *testing.B) {
	ctx := context.Background()

	var commitGraphRepoFactory interfaces.CommitGraphRepositoryFactory
	var gitFSFactory interfaces.GitFSFactory

	b.StopTimer()
	dc := di.WireUp(b, fx.Populate(&commitGraphRepoFactory, &gitFSFactory))
	dc.World.MakeOrgs(b)

	repoID, _ := dc.World.ImportFixture(b, "https://github.com/docker/compose.git")

	commitGraphRepo := commitGraphRepoFactory.Build([]uint64{repoID})

	gr, err := commitGraphRepo.GetCommitGraphNodes(ctx, false)
	require.NoError(b, err)

	allHashes := functools.Map(gr, func(e entities.CommitGraphNode) plumbing.Hash { return e.Hash })

	b.StartTimer()

	bbs := []struct {
		name      string
		nextChunk func() []plumbing.Hash
	}{
		{
			name:      "all commits",
			nextChunk: func() []plumbing.Hash { return allHashes },
		},
		{
			name: "small chunks",
			nextChunk: func() []plumbing.Hash {
				start := rand.Int31n(int32(len(allHashes) - 100))
				end := start + 1 + rand.Int31n(80)
				return allHashes[start:end]
			},
		},
		{
			name: "single commit",
			nextChunk: func() []plumbing.Hash {
				start := rand.Int31n(int32(len(allHashes) - 2))
				return allHashes[start : start+1]
			},
		},
	}

	rand.Seed(42)

	for _, bb := range bbs {
		b.Run(bb.name, func(b *testing.B) {
			loader := gitFSFactory.Build(repoID)

			gitFS, closer, err := loader.Load(ctx)
			require.NoError(b, err)
			defer utils.HandleErrors(&err, closer)

			for i := 0; i < b.N; i++ {
				hashes := bb.nextChunk()
				cmts, err := gitFS.GetCommitsByHashBulk(ctx, hashes)
				require.NoError(b, err)
				require.Equal(b, len(hashes), len(cmts))
				// walkCommits(b, bb.loadBloom, bb.loadTrees, headHash, metadataRepo)

			}
		})
	}
}
