package integrationtests

import (
	"context"
	"gitcore/internal/utils/bloom"
	"github.com/stretchr/testify/require"
)

func (suite *RwApiTestSuite) TestGenNumAndBloomFilter() {
	ctx := context.Background()

	t := suite.T()

	err := suite.CommitGraphRepoFactory.ResetGennum(ctx, suite.repos.Alpha.ID)
	require.NoError(t, err)

	err = suite.IndexRepositoryFactory.
		WithHeartBeat(func(str string) error {
			return ctx.Err()
		}).
		Build(suite.repos.Alpha.ID).
		Index(ctx)
	require.NoError(t, err)

	commitGraphRepo := suite.CommitGraphRepoFactory.Build([]uint64{suite.repos.Alpha.ID})

	commitGraph, err := commitGraphRepo.GetCommitGraphNodes(ctx, true)
	require.NoError(t, err)

	type commitInfo struct {
		gennum          int
		expectedFiles   []string
		unexpectedFiles []string
	}

	/**
	* 6ecf0ef - (10 years ago) vendor stuff - Máximo Cuadros Ortiz (HEAD -> master)
	| * e8d3ffa - (10 years ago) some code in a branch - Máximo Cuadros Ortiz (keep-me, branch)
	|/
	* 918c48b - (10 years ago) some code - Máximo Cuadros Ortiz
	* af2d6a6 - (10 years ago) some json - Máximo Cuadros Ortiz
	*   1669dce - (10 years ago) Merge branch 'master' of github.com:tyba/git-fixture - Máximo Cuadros Ortiz
	|\
	| *   a5b8b09 - (10 years ago) Merge pull request #1 from dripolles/feature - Máximo Cuadros
	| |\
	| | * b8e471f - (10 years ago) Creating changelog - Daniel Ripolles
	| |/
	* / 35e8510 - (10 years ago) binary file - Máximo Cuadros Ortiz
	|/
	* b029517 - (10 years ago) Initial commit - Máximo Cuadros
	*/
	gnMapExpected := map[string]commitInfo{
		"b029517f6300c2da0f4b651b8642506cd6aaf45d": commitInfo{1,
			[]string{},
			[]string{},
		}, // root
		"35e85108805c84807bc66a02d91535e1e24b38b9": {2,
			[]string{"binary.jpg"},
			[]string{"CHANGELOG"},
		},
		"b8e471f58bcbca63b07bda20e428190409c2db47": {2,
			[]string{"CHANGELOG"},
			[]string{"binary.jpg"},
		},
		"a5b8b09e2f8fcb0bb99d3ccb0958157b40890d69": {3,
			[]string{"CHANGELOG"},
			[]string{"binary.jpg"},
		},
		"1669dce138d9b841a518c64b10914d88f5e488ea": {4,
			[]string{"CHANGELOG"},
			[]string{"binary.jpg"},
		},
		"af2d6a6954d532f8ffb47615169c8fdf9d383a1a": {5,
			[]string{"json/short.json", "json/long.json", "json"},
			[]string{"binary.jpg", "go", "php"},
		},
		"918c48b83bd081e863dbe1b80f8998f058cd8294": {6,
			[]string{"go/example.go", "php/crappy.php"},
			[]string{"binary.jpg"},
		},
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5": {7,
			[]string{"vendor/foo.go", "vendor"},
			[]string{"go", "php"},
		},
		"e8d3ffab552895c19b9fcf7aa264d277cde33881": {7,
			[]string{"README"},
			[]string{"binary.jpg"},
		},
	}

	for _, b := range commitGraph {
		expected, ok := gnMapExpected[b.Hash.String()]
		require.True(t, ok, "No commit %s in commit graph", b.Hash.String())
		require.Equal(t, expected.gennum, b.GenerationNumber)

		blf, err := bloom.Load(b.Bloom)
		require.NoError(t, err)

		for _, s := range expected.expectedFiles {
			require.True(t, blf.CanContain(s), "commit %s should be able to contain %s", b.Hash.String(), s)
		}

		for _, s := range expected.unexpectedFiles {
			require.False(t, blf.CanContain(s), "commit %s should not contain %s", b.Hash.String(), s)
		}
	}
}
