package integrationtests

import (
	"common/cgit"
	"common/functools"
	"common/logging"
	"context"
	"fmt"
	"gitcore/internal/entities"
	"gitcore/internal/git/packfile/delta"
	"gitcore/internal/git/packfile/parser"
	"gitcore/internal/testutils"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"io"
	"os"
	"path"
	"strconv"
	"strings"
	"testing"
	"time"
)

const totalPacksToGenerate = 6

func (suite *RepoApiTestSuite) TestRepacker() {
	t := suite.T()
	t.Skip("flapping test")
	oldRepackEnabled := suite.cfg.Tasks.Repack.Enabled
	oldRepackDelay := suite.cfg.Tasks.Repack.Delay
	oldKeepObsoletePacks := suite.cfg.Tasks.Repack.KeepObsoletePacksFor
	suite.cfg.Tasks.Repack.Enabled = true
	suite.cfg.Tasks.Repack.Delay = time.Microsecond
	suite.cfg.Tasks.Repack.KeepObsoletePacksFor = time.Nanosecond

	defer func() {
		suite.cfg.Tasks.Repack.Enabled = oldRepackEnabled
		suite.cfg.Tasks.Repack.Delay = oldRepackDelay
		suite.cfg.Tasks.Repack.KeepObsoletePacksFor = oldKeepObsoletePacks
	}()

	ctx := context.Background()

	orgSlug := "testrepackorg"
	org := suite.OrganizationFixture(0, orgSlug, suite.users.Admin, nil)
	repoSlug := "repack2" + "-" + uuid.New().String()
	cloneURL, _ := suite.emptyRepo(orgSlug, repoSlug, suite.users.Admin)

	initStorageUsage := suite.getStorageUsage(t, org.ID)

	repo, err := suite.RepoRepo.GetRepository(ctx, orgSlug, repoSlug)
	require.NoError(t, err)

	metadataRepo := suite.MetaDataRepoFactory.Build(repo.ID)

	packs, err := metadataRepo.GetAllPacks(ctx)
	require.NoError(t, err)
	require.Len(t, packs, 0)

	suite.cfg.Tasks.Repack.Enabled = false

	_ = fillRepo(t, cloneURL)

	afterPushStorageUsage := suite.getStorageUsage(t, org.ID)
	require.Greater(t, afterPushStorageUsage, initStorageUsage)

	logging.Info(ctx, "got storage usage: %d", afterPushStorageUsage)

	suite.cfg.Tasks.Repack.Enabled = true

	err = suite.RepoService.ScheduleMaintenance(ctx, repo.OrgID, repo.ID, repo.Visibility, false)
	require.NoError(t, err)

	err = suite.RepoService.WaitForMaintenance(ctx, repo.ID)
	require.NoError(t, err)

	afterRepackStorageUsage := suite.getStorageUsage(t, org.ID)
	require.Less(t, afterRepackStorageUsage, afterPushStorageUsage)

	packs, err = metadataRepo.GetAllPacks(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, packs, "we should pack all the previous packs")
	require.Equal(t, afterRepackStorageUsage-initStorageUsage, getPackSizes(packs))

	for _, pack := range packs {
		f, err := suite.PackCache.GetOrDownload(suite.StorageBackend.PackKey(repo.ID, pack.ID), nil)
		require.NoError(t, err)

		observer := &isBinaryObserver{}
		packParser, err := parser.NewParser(
			parser.StubGitFS{},
			io.Discard,
			parser.WithObservers(observer),
			parser.WithAllowThinPack(),
		)
		require.NoError(t, err)

		_, _, err = packParser.Parse(ctx, f)
		require.NoError(t, err)

		c := pack.IsBinary.ToArray()
		logging.Debug(ctx, "%v", c)

		require.Equal(t, functools.SliceToSet(c), functools.SliceToSet(observer.isBinary))

		require.NoError(t, f.Close())
	}

	// Then, try to do a full clone
	tmpDir := testutils.TempDir(t, "", "repack*")
	cg2 := cgit.NewCGit(tmpDir).WithAuthToken(testutils.FakeIAMAuthToken(testutils.UserIdentities.Admin))
	err = cg2.MirrorCloneRepo(ctx, cloneURL, path.Join(tmpDir, "ttt"))
	require.NoError(t, err)
}

func fillRepo(t *testing.T, cloneURL string) cgit.CGit {
	tmpDir := testutils.TempDir(t, "", "repack*")
	repoPath := tmpDir

	// clone repo
	cg := cgit.NewCGit(tmpDir).WithAuthToken(testutils.FakeIAMAuthToken(testutils.UserIdentities.Admin))
	logging.Info(nil, cg.Must(t, "init", "."))
	logging.Info(nil, cg.Must(t, "remote", "add", "origin", cloneURL))

	// push file to main first (can`t push empty branch to empty repo)
	logging.Info(nil, cg.Must(t, "checkout", "-b", "main"))
	logging.Info(nil, cg.Must(t, "config", "user.email", "you@example.com"))
	logging.Info(nil, cg.Must(t, "config", "user.name", "Your Name"))

	subPath := "a1/a2/a3"
	require.NoError(t, os.MkdirAll(path.Join(repoPath, subPath), 0755))

	for p := 0; p < totalPacksToGenerate; p++ {
		for i := 1000 + p; i < 1015-p; i++ {
			if p == 0 {
				var firstLine string
				if i == 1000+(totalPacksToGenerate/2) {
					// this will make one of the files "look" binary. We choose file in the middle to make sure it will
					// appear in more than one packfile.
					firstLine = strings.Repeat(string(rune(0))+"a", 50)
				} else {
					firstLine = strings.Repeat("a", 100)
				}

				require.NoError(t, os.WriteFile(
					path.Join(repoPath, subPath, fmt.Sprintf("file%d.txt", i)), []byte(firstLine+fmt.Sprintf("file%d.txt", i)), 0666))
			} else {
				f, err := os.OpenFile(
					path.Join(repoPath, subPath, fmt.Sprintf("file%d.txt", i)), os.O_WRONLY|os.O_APPEND, 0666)
				require.NoError(t, err)

				_, err = f.WriteString(strconv.Itoa(i))
				require.NoError(t, err)

				err = f.Close()
				require.NoError(t, err)
			}
		}

		cg.Must(t, "add", "*")
		cg.Must(t, "commit", "-m", fmt.Sprintf("commit%d", p))

		if p == 0 {
			cg.Must(t, "push", "-u", "origin", "main")
		} else {
			cg.Must(t, "push")
		}
	}

	logging.Info(nil, cg.Must(t, "log"))
	return cg
}

func (suite *RepoApiTestSuite) getStorageUsage(t *testing.T, orgID uint64) uint64 {
	quotaStoragePublic, err := suite.quotaService.Get(context.Background(), orgID, entities.Quotas.ObjectStorageSize)
	require.NoError(t, err)
	quotaStoragePrivate, err := suite.quotaService.Get(context.Background(), orgID, entities.Quotas.ObjectStoragePrivateSize)
	require.NoError(t, err)

	return uint64(quotaStoragePublic.Usage + quotaStoragePrivate.Usage)
}

func getPackSizes(packs []entities.Pack) uint64 {
	var size uint64
	for _, pack := range packs {
		size += pack.Size
	}
	return size
}

// isBinaryObserver is just a helper for this test that collects positions of binary objects in packfile
type isBinaryObserver struct {
	isBinary []uint32
}

func (p *isBinaryObserver) OnHeader(_ context.Context, _ uint32) error {
	return nil
}

func (p *isBinaryObserver) OnParsingComplete(_ context.Context) error {
	return nil
}

func (p *isBinaryObserver) OnFooter(_ plumbing.Hash) error {
	return nil
}

func (p *isBinaryObserver) OnInflatedObject(_ context.Context,
	info *parser.ObjectInfo,
	_ *delta.FileBackedBuffer,
	details *parser.ParsedObjectDetails) error {

	if details.IsBinary {
		p.isBinary = append(p.isBinary, info.Index)
	}

	return nil
}
