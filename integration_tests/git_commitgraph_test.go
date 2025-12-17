package integrationtests

import (
	"common/functools"
	"context"
	"gitcore/internal/di"
	"gitcore/internal/entities"
	"gitcore/internal/interfaces"
	"gitcore/internal/testutils"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"
	"testing"
)

func BenchmarkWalkCommits(b *testing.B) {
	b.StopTimer()
	metadataRepo, commitGraph, gitFSLoader, headHash := prepareTest(b)
	gitFS, closer, err := gitFSLoader.Load(context.Background())
	require.NoError(b, err)
	defer func() {
		require.NoError(b, closer())
	}()

	b.StartTimer()

	bbs := []struct {
		name      string
		loadBloom bool
	}{
		{name: "bloom", loadBloom: true},
		{name: "no bloom", loadBloom: false},
	}

	for _, bb := range bbs {
		b.Run(bb.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				walkCommits(b, gitFS, bb.loadBloom, headHash, metadataRepo, commitGraph)
			}
		})
	}
}

func prepareTest(t testing.TB) (interfaces.MetadataRepository, interfaces.CommitGraph, interfaces.GitFSLoader, plumbing.Hash) {
	var metadataRepoFactory interfaces.MetadataRepositoryFactory
	dc := di.WireUp(t, fx.Populate(&metadataRepoFactory))
	dc.World.MakeOrgs(t)
	repoID, headHash := dc.World.ImportFixture(t, testutils.BasicRepo)

	return metadataRepoFactory.Build(repoID), dc.CommitGraphFactory.Build(), dc.GitFSFactory.Build(repoID), headHash
}

func walkCommits(t testing.TB, gitFS interfaces.GitFS, loadBloomAndGen bool, headHash plumbing.Hash, metadataRepo interfaces.MetadataRepository, commitGraph interfaces.CommitGraph) {
	ctx := context.Background()
	require.NoError(t, commitGraph.Init(ctx, gitFS, loadBloomAndGen))

	haves := map[plumbing.Hash]struct{}{}
	err := commitGraph.WalkCommits(ctx, []plumbing.Hash{headHash},
		func(node *entities.CommitGraphNode, depth int) (bool, error) {
			haves[node.Hash] = struct{}{}
			return true, nil
		}, true)
	require.NoError(t, err)

	var havesSlice []plumbing.Hash
	for hash := range haves {
		havesSlice = append(havesSlice, hash)
	}

	haveCommits, err := gitFS.GetCommitsByHashBulk(ctx, havesSlice)
	require.NoError(t, err)

	testutils.AssertCommitsIgnoreOrder(t, []string{
		"6ecf0ef2c2df",
		"918c48b83bd0",
		"af2d6a6954d5",
		"1669dce138d9",
		"a5b8b09e2f8f",
		"35e85108805c",
		"b8e471f58bcb",
		"b029517f6300",
	}, functools.Values(haveCommits))

	bases := map[plumbing.Hash]struct{}{}
	wants := map[plumbing.Hash]struct{}{}

	err = commitGraph.WalkCommits(
		ctx,
		[]plumbing.Hash{plumbing.NewHash("e8d3ffab552895c19b9fcf7aa264d277cde33881")},
		func(node *entities.CommitGraphNode, depth int) (bool, error) {
			if _, ok := haves[node.Hash]; ok {
				bases[node.Hash] = struct{}{}
				return false, nil
			}
			wants[node.Hash] = struct{}{}
			return true, nil
		},
		true,
	)
	require.NoError(t, err)

	var wantsSlice []plumbing.Hash
	for hash := range wants {
		wantsSlice = append(wantsSlice, hash)
	}
	wantsCommits, err := gitFS.GetCommitsByHashBulk(ctx, wantsSlice)
	require.NoError(t, err)

	testutils.AssertCommitsIgnoreOrder(t, []string{"e8d3ffab5528"}, functools.Values(wantsCommits))

	var basesSlice []plumbing.Hash
	for hash := range bases {
		basesSlice = append(basesSlice, hash)
	}
	baseCommits, err := gitFS.GetCommitsByHashBulk(ctx, basesSlice)
	require.NoError(t, err)

	testutils.AssertCommitsIgnoreOrder(t, []string{"918c48b83bd0"}, functools.Values(baseCommits))
}

func TestWalkCommits(t *testing.T) {
	metadataRepo, commitGraph, gitFSLoader, headHash := prepareTest(t)
	gitFS, closer, err := gitFSLoader.Load(context.Background())
	require.NoError(t, err)
	defer func() {
		require.NoError(t, closer())
	}()

	walkCommits(t, gitFS, true, headHash, metadataRepo, commitGraph)
}

// Demo Repo structure
// * 1c584ac
// |
// * 5a5a3ae
// |
// * 6ecf0ef
// |
// * 918c48b
// |
// * af2d6a6
// |
// *   1669dce
// |\
// | |
// | *   a5b8b09
// | |\
// | | |
// | | * b8e471f
// | |/
// | |
// * | 35e8510
// |/
// |
// * b029517

func TestGetDirectory(t *testing.T) {
	ctx := context.Background()

	var pcd interfaces.PackCacheDiagnostics
	var pif interfaces.PathInfoFactory

	dc := di.WireUp(t, fx.Populate(&pcd), fx.Populate(&pif))
	dc.World.MakeOrgs(t)
	repoID, refHash := dc.World.ImportFixture(t, "basic2.git")

	type testcase struct {
		path          string
		expectedState map[string]string
	}

	cases := []testcase{
		{
			path: "/",
			expectedState: map[string]string{
				"/":          "1c584accce74705f947258640ad87fe55f517815",
				".gitignore": "b029517f6300c2da0f4b651b8642506cd6aaf45d",
				"binary.jpg": "35e85108805c84807bc66a02d91535e1e24b38b9",
				"CHANGELOG":  "b8e471f58bcbca63b07bda20e428190409c2db47",
				"go":         "918c48b83bd081e863dbe1b80f8998f058cd8294",
				"json":       "af2d6a6954d532f8ffb47615169c8fdf9d383a1a",
				"LICENSE":    "b029517f6300c2da0f4b651b8642506cd6aaf45d",
				"php":        "918c48b83bd081e863dbe1b80f8998f058cd8294",
				"vendor":     "6ecf0ef2c2dffb796033e5a02219af86ec6584e5",
			},
		},
		{
			path: "go",
			expectedState: map[string]string{
				"go":            "918c48b83bd081e863dbe1b80f8998f058cd8294",
				"go/example.go": "918c48b83bd081e863dbe1b80f8998f058cd8294",
			},
		},
		{
			path: "vendor",
			expectedState: map[string]string{
				"vendor":        "6ecf0ef2c2dffb796033e5a02219af86ec6584e5",
				"vendor/foo.go": "6ecf0ef2c2dffb796033e5a02219af86ec6584e5",
			},
		},
		// non-dir should work too
		{
			path: "vendor/foo.go",
			expectedState: map[string]string{
				"vendor/foo.go": "6ecf0ef2c2dffb796033e5a02219af86ec6584e5",
			},
		},
	}

	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			func() {
				gitFS, closer, err := dc.GitFSFactory.Build(repoID).Load(ctx)
				require.NoError(t, err)
				defer func() {
					require.NoError(t, closer())
				}()

				pi, err := pif.Build(ctx, repoID, gitFS)
				require.NoError(t, err)

				_, _, lastCommits, err := pi.GetLastCommitsBulk(ctx, c.path, refHash)
				require.NoError(t, err)

				n := make(map[string]string, len(lastCommits))
				for fullPath, commit := range lastCommits {
					n[fullPath] = commit.String()
				}
				require.Equal(t, c.expectedState, n)
			}()

			require.Empty(t, pcd.DanglingDescriptorsKeys(), "dangling descriptor pointers shouldn't be introduced")
			require.Equal(t, int64(pcd.GetTotalEntriesSize()), int64(pcd.GetOccupiedMemorySize()))
		})
	}
}
