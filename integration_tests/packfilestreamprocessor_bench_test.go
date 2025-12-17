package integrationtests

import (
	"common/cgit"
	"common/logging"
	commonpg "common/postgres"
	"context"
	"crypto"
	gosha "crypto/sha1"
	"gitcore/internal/adapters/postgres"
	"gitcore/internal/config"
	"gitcore/internal/entities"
	"gitcore/internal/git/gitfs"
	yaparser "gitcore/internal/git/packfile/parser"
	"gitcore/internal/interfaces"
	"gitcore/internal/testutils"
	"github.com/go-git/go-git/v5/plumbing/hash"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"io"
	"math/rand/v2"
	"os"
	"path"
	"slices"
	"strings"
	"testing"
)

func EnsurePackFileLoaded(t testing.TB, url, branch string) string {
	ctx := context.Background()

	org, repo, err := testutils.ParseOrgAndRepoFromURL(url)
	require.NoError(t, err)

	localPath := path.Join(os.TempDir(), org, repo)
	packPath := path.Join(localPath, ".git", "objects", "pack")
	_, err = os.Stat(packPath)

	if os.IsNotExist(err) {
		// Checkout
		logging.Info(ctx, "Be patient, big repo is being cloned...")
		err = cgit.CloneRef(ctx, url, localPath, &branch)
	}
	require.NoError(t, err)

	ppdir, err := os.ReadDir(packPath)
	require.NoError(t, err)

	idx := slices.IndexFunc(ppdir, func(e os.DirEntry) bool {
		return !e.IsDir() && strings.HasSuffix(e.Name(), ".pack")
	})
	require.NotEqual(t, -1, idx)

	return path.Join(packPath, ppdir[idx].Name())
}

func BenchmarkLoadPackfile(b *testing.B) {
	b.StopTimer()

	err := hash.RegisterHash(crypto.SHA1, gosha.New)
	require.NoError(b, err)

	rdr, err := os.Open(EnsurePackFileLoaded(b, "https://github.com/yandex/smart.git", "smart_3_14"))
	require.NoError(b, err)

	observer := yaparser.NewStubObserver()

	ctx := context.Background()
	cfg, err := config.GetUnittestAppConfig()
	require.NoError(b, err)

	pool := postgres.MakeUnittestPgxPool(b)
	gormDB := commonpg.NewGormFactory(pool)
	txManager := postgres.NewTransactionManager(gormDB)
	repo := postgres.NewRepositoryRepository(cfg, pool, gormDB, txManager, postgres.NewMigratedRepositoryRepository(gormDB))
	org := postgres.NewOrganizationRepository(pool, gormDB, txManager)

	orgID, err := org.CreateOrganization(
		ctx,
		&entities.Organization{
			Slug:       "yandex",
			Identity:   entities.NewOrganizationIdentity("yandex"),
			Visibility: entities.Visibilities.Public,
		},
	)
	require.NoError(b, err)
	require.NotZero(b, orgID)

	repoID, err := repo.CreateRepository(ctx, &interfaces.CreateRepositoryArgs{
		Name:       "alpha",
		Slug:       "alpha",
		OrgID:      orgID,
		Visibility: entities.Visibilities.Public,
	})
	require.NoError(b, err)

	metadataRepoFactory := postgres.NewMetadataRepositoryFactory(pool, gormDB)
	hist := prometheus.NewHistogram(prometheus.HistogramOpts{Name: "some"})

	gitFSLoader := gitfs.NewGitFSLoader(gitfs.GitFSLoaderDI{
		FactoryDI: gitfs.FactoryDI{
			MetadataRepositoryFactory: metadataRepoFactory,
		},
		RepositoryRepository: repo,
	}, gitfs.GitFSDeps{
		StorageBackend:      testutils.NewNopStorageBackend(),
		PackCache:           nil,
		PacksCountHistogram: hist,
	}, repoID)
	gitFS, closer, err := gitFSLoader.Load(ctx) // gitfs.newGitFS(, , nil, hist, repoID)
	require.NoError(b, err)
	defer func() {
		require.NoError(b, closer())
	}()

	prsr, err := yaparser.NewParser(
		gitFS, io.Discard,
		yaparser.WithObservers(observer),
	)
	require.NoError(b, err)

	b.StartTimer()

	for i := 0; i < b.N; i++ {
		_, _, err := prsr.ParseStream(ctx, rdr)
		require.NoError(b, err)
	}
}

func BenchmarkShaOverhead(b *testing.B) {
	benches := []struct {
		name   string
		hasher func() hash.Hash
	}{
		{"gostd", func() hash.Hash { return gosha.New() }},
		{"hardened", func() hash.Hash { return hash.New(crypto.SHA1) }},
	}

	for _, bb := range benches {
		b.Run(bb.name, func(b *testing.B) {
			data := []byte{1, 2, 3, 4}

			for i := 0; i < b.N; i++ {
				hs := bb.hasher()
				_, _ = hs.Write(data)
				hs.Sum(nil)
			}
		})
	}
}

func BenchmarkSha1(b *testing.B) {
	benches := []struct {
		name   string
		hasher func() hash.Hash
	}{
		{"gostd", func() hash.Hash { return gosha.New() }},
		{"hardened", func() hash.Hash { return hash.New(crypto.SHA1) }},
	}

	for _, bb := range benches {
		b.Run(bb.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				packname := EnsurePackFileLoaded(b, "https://github.com/yandex/smart.git", "smart_3_14")

				rdr, err := os.Open(packname)
				require.NoError(b, err)

				stat, err := os.Stat(packname)
				require.NoError(b, err)

				for i = 0; i < 900_000; i++ {
					_, err = rdr.Seek(rand.Int64N(stat.Size()-1), 0)
					require.NoError(b, err)

					buf := make([]byte, 10+rand.IntN(4086))

					hasher := bb.hasher()
					n, err := rdr.Read(buf)
					if n > 0 {
						_, _ = hasher.Write(buf)

						_ = hasher.Sum([]byte{})
					}
					require.NoError(b, err)
				}
			}
		})
	}
}
