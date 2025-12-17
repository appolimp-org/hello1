package integrationtests

import (
	"context"
	"gitcore/internal/di"
	"gitcore/internal/interfaces"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/fx"
)

func TestPackFileParsing(t *testing.T) {
	ctx := context.Background()

	var metadataRepoFactory interfaces.MetadataRepositoryFactory
	var gitFSFactory interfaces.GitFSFactory

	dc := di.WireUp(t, fx.Populate(&metadataRepoFactory, &gitFSFactory))
	dc.World.MakeOrgs(t)

	fixtureNames := map[string]struct{}{
		"basic.git":     {},
		"basic1.git":    {},
		"history.git":   {},
		"submodule.git": {},
		"tags.git":      {},
		"thinpack.git":  {},
		"odyssey.git":   {},
	}

	for fixtureName := range fixtureNames {
		t.Run(fixtureName, func(t *testing.T) {
			repoID, _ := dc.World.ImportFixture(t, fixtureName)

			gitFS, closer, err := gitFSFactory.Build(repoID).Load(ctx)
			require.NoError(t, err)
			defer func() {
				require.NoError(t, closer())
			}()

			packs, err := gitFS.Packs().GetActivePacksIDs(ctx)
			require.NoError(t, err)

			allObjs, err := gitFS.Packs().GetUniqObjectRefs(ctx, packs)
			require.NoError(t, err)

			for _, obj := range allObjs {
				content, err := gitFS.ReadFileByRef(ctx, obj)
				require.NoError(t, err)

				hdr, _, err := gitFS.Packs().GetObjectHeaderByOffset(ctx, obj.PackID, obj.Offset)
				require.NoError(t, err)
				require.True(t, hdr.Type.Valid())

				// println(obj.Offset, obj.Hash.String(), hdr.Type.String(), len(content))
				sz, err := gitFS.Packs().GetObjectSize(ctx, obj.PackID, obj.Offset)
				require.NoError(t, err)

				require.Equal(t, int64(len(content)), sz)
			}
		})
	}
}
