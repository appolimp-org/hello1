package integrationtests

import (
	"bytes"
	"common/cgit"
	"common/functools"
	"common/logging"
	"common/testutils/yarequire"
	yautils "common/utils"
	"common/utils/rolesgenerator/iam"
	"context"
	"fmt"
	"gitcore/internal/access/common"
	"gitcore/internal/entities"
	"gitcore/internal/entities/signals"
	except "gitcore/internal/exceptions"
	"gitcore/internal/git"
	"gitcore/internal/git/zlib"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/housekeeping"
	"gitcore/internal/services/access"
	"gitcore/internal/testutils"
	"github.com/go-git/go-git/v5/plumbing"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"

	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/testing/protocmp"

	"io"
	"math"
	"os"
	"path"
	"runtime/debug"
	"strconv"
	"strings"
	"testing"

	"github.com/alitto/pond"
	"github.com/cockroachdb/errors"
	"github.com/fatih/color"
	"github.com/go-git/go-billy/v5/osfs"
	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/server"
	"github.com/go-git/go-git/v5/storage/filesystem"
	"github.com/go-resty/resty/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// Here we don't test any our code but rather benchmark go-git code
func BenchmarkBaselineUploadPack(t *testing.B) {
	ctx := context.Background()

	raw := `004dwant 5fcfff08210d191a4bc028fe36dcaedfd1aca71f ofs-delta agent=git/2.34.1
00000009done`
	upr := packp.NewUploadPackRequest()
	err := upr.Decode(bytes.NewReader([]byte(raw)))

	require.NoError(t, err)
	gitfs := osfs.New(testutils.GetDataPath("odyssey.git"))

	s := filesystem.NewStorage(gitfs, cache.NewObjectLRUDefault())
	ld := git.NewLoader(s)
	tr := server.NewServer(ld)

	endpoint, err := transport.NewEndpoint("/")
	require.NoError(t, err)

	sess, err := tr.NewUploadPackSession(endpoint, nil)
	require.NoError(t, err)

	defer func() {
		require.NoError(t, sess.Close())
	}()

	//perfcheck.PerfCheck.Reset()
	res, err := sess.UploadPack(ctx, upr)
	require.NoError(t, err)

	//b := &testutils.ByteStreamSnooperWriter{W: io.Discard, Limit: 256}
	err = res.Encode(io.Discard)
	require.NoError(t, err)

	//perfcheck.PerfCheck.Print()
}

// TODO: тест может флапать из-за того что zlib.GetPoolCounter у нас глобальный и может использоваться в том числе бекграундными тасками (repo index)
func (suite *LargeRepoApiTestSuite) TestPushClone() {

	color.NoColor = false
	t := suite.T()

	orgSlug := "yandex"
	slug := "odyssey"
	source := "yandex/odyssey"
	//slug := "git-fixtures/basic"

	repoURL := suite.RepoURL(orgSlug, slug)

	remote, err := cgit.EnsureRemoteRepo("https://github.com/" + source)
	require.NoError(suite.T(), err)

	cg := cgit.NewCGit(remote).WithAuthToken(testutils.FakeIAMAuthToken(testutils.UserIdentities.Admin))
	cg.MustSetOrigin(t, repoURL)

	// master
	require.Equal(t, 0, zlib.GetPoolCounter())
	// TODO: reconsider it. Now our cache is used in this process
	// require.False(t, suite.PackCache.HasDanglingDescriptors())
	color.Cyan("Pushing...")
	logging.Info(context.Background(), cg.Must(t, "push", "origin"))
	require.Equal(t, 0, zlib.GetPoolCounter()) // must not leak resources
	// TODO: reconsider it. Now our cache is used in this process
	// require.False(t, suite.PackCache.HasDanglingDescriptors())

	color.Cyan("Pushing rest of branches...")
	// push rest of branches (thin pack)
	logging.Info(nil, cg.Must(t, "push", "--all", "origin"))

	require.Equal(t, 0, zlib.GetPoolCounter()) // must not leak resources

	resp, err := suite.client.R().Get(fmt.Sprintf("/api/v1/repos/%s/%s/branches", orgSlug, slug))

	require.NoError(t, err)
	logging.Info(nil, "resp: %s ", string(resp.Body()))

	// clone back to disk --->
	color.Cyan("Cloning...")

	//trace.SetTarget(trace.Packet) TRACE GO-GIT PROTO

	tmpDir := t.TempDir()
	cg2 := cgit.NewCGit(tmpDir)

	logging.Info(context.Background(), cg2.Must(t, "clone", repoURL, "."))

	// require.Equal(t, zlib.GetPoolCounter(), 0)
	// TODO: reconsider it. Now our cache is used in this process
	// require.False(t, suite.PackCache.HasDanglingDescriptors())
}

func (suite *RepoApiTestSuite) TestPushAverageRepoBaseline() {
	t := suite.T()

	_, orgSlug, repoSlug := suite.makeRandomRepo(t, suite.users.Kopatych)

	tmpDir := t.TempDir()

	cgRemote := cgit.NewCGit(tmpDir).WithAuthToken(testutils.FakeIAMAuthToken(suite.users.Kopatych.Identity))
	cgRemote.Must(t, "clone", "--bare", "file://"+testutils.GetDataPath("odyssey.git"), ".")
	cgRemote.Must(t, "remote", "add", "o2", suite.HTTPSProtocol().RepoURL(orgSlug, repoSlug))

	cgRemote.Must(t, "push", "--all", "o2")
}

func (suite *RepoApiTestSuite) TestParallelPush() {
	t := suite.T()

	t.Skip("wait for fix OO-1703")

	_, org, repo := suite.makeRandomRepo(t, suite.users.Kopatych)
	url := suite.HTTPSProtocol().RepoURL(org, repo)

	crr := suite.repos.Crisscross.RepoFullSlug()
	urlBase := suite.HTTPSProtocol().RepoURL(crr.OrgSlug, crr.RepoSlug)

	remote, err := cgit.EnsureRemoteRepo(urlBase)
	require.NoError(t, err)

	cg := cgit.NewCGit(remote).WithAuthToken(testutils.FakeIAMAuthToken(testutils.UserIdentities.Kopatych))
	origin, err := yautils.MakeRandomString("origin", 5)
	require.NoError(t, err)

	sout, serr, err := cg.Exec("remote", "add", origin, url)
	require.NoError(t, err, sout, serr)

	branches := []string{
		"A",
		"B",
		"C",
		"D",
		"E",
		"F",
		"G",
		"main",
		"other",
	}

	ctx := context.Background()
	ctx, cancel := context.WithCancelCause(ctx)
	pool := pond.New(len(branches), 10, pond.PanicHandler(func(p interface{}) {
		logging.Debug(ctx, "Task panicked: %v; aborting rest of jobs\n Stacktrace: %v\n", p, string(debug.Stack()))
		cancel(errors.New("git push panicked"))
	}))
	group, _ := pool.GroupContext(ctx)

	pushBranch := func(branch string) {
		group.Submit(func() error {
			// Create separate cgit instance to avoid askpass clash
			cg := cgit.NewCGit(remote).WithAuthToken(testutils.FakeIAMAuthToken(testutils.UserIdentities.Kopatych))

			logging.Debug(ctx, "Started to push %s\n", branch)
			return cg.ExecNoCapture("push", "-u", origin, branch)
		})
	}

	for _, b := range branches {
		pushBranch(b)
	}

	err = group.Wait()
	require.NoError(t, err)

	pool.Stop()

	// Try to clone uploaded
	cgClone := cgit.NewCGit(
		testutils.TempDir(t, "", "cloned")).
		WithAuthToken(testutils.FakeIAMAuthToken(testutils.UserIdentities.Kopatych))
	sout, serr, err = cgClone.Exec("clone", "--mirror", url, "cloned")
	require.NoError(t, err, sout, serr)
}

func (suite *RepoApiTestSuite) TestPushThinPack() {
	t := suite.T()
	ctx := context.Background()

	orgSlug := "yandex"
	slug := "beta"
	suite.addOrgRole(t, suite.users.Kopatych, suite.orgs.Yandex, iam.Roles.InternalOrganizationManagerMember)
	repoURL, repoID := suite.emptyRepo(orgSlug, slug, suite.users.Kopatych)
	commit := "branch"
	filepath := "README"

	getRaw := func(rev, path, slug string) (*resty.Response, error) {
		return suite.client.R().
			SetQueryParam("rev", rev).
			SetQueryParam("path", path).
			Get(fmt.Sprintf("/api/v1/repos/%s/%s/raw", orgSlug, slug))
	}

	tmpDir := testutils.TempDir(t, "", "basic1")
	cg := cgit.NewCGit(tmpDir)
	cg.Must(t, "clone", testutils.GetDataPath("basic1.git"), "basic1")
	cg = cgit.NewCGit(path.Join(tmpDir, "basic1")).WithAuthToken(testutils.FakeIAMAuthToken(testutils.UserIdentities.Kopatych))
	err := cg.CloneAllBranches()
	require.NoError(t, err)
	cg.MustSetOrigin(t, repoURL)
	// master
	logging.Info(nil, cg.Must(t, "push", "origin"))

	resp, err := getRaw(commit, filepath, slug)
	require.NoError(t, err)
	require.Equal(t, 404, resp.StatusCode())

	// branch, pushed as "Thin Pack"
	logging.Info(nil, cg.Must(t, "push", "--all", "origin"))

	resp, err = suite.client.R().Get(fmt.Sprintf("/api/v1/repos/%s/%s/branches", orgSlug, slug))
	require.NoError(t, err)
	logging.Info(nil, "resp: %s ", string(resp.Body()))

	expected := cg.Must(t, "show", fmt.Sprintf("%s:%s", commit, filepath))

	resp, err = getRaw(commit, filepath, slug)
	require.NoError(t, err)
	require.Equal(t, expected, string(resp.Body()))

	md := suite.MetaDataRepoFactory.Build(repoID)

	allPacks, err := md.GetAllPacks(ctx)
	require.NoError(t, err)
	require.Len(t, allPacks, 2, "there should be original pack + thinpack")

	allPackExtRefs, err := md.GetPackFilesExternalRefs(ctx,
		functools.Map(allPacks, func(p entities.Pack) uint64 { return p.ID }))
	require.NoError(t, err)
	require.Len(t, allPackExtRefs, 1)
	// There should be ony one thinpacked
	require.Len(t, allPackExtRefs[functools.Keys(allPackExtRefs)[0]], 1)
	require.Equal(t, allPackExtRefs[functools.Keys(allPackExtRefs)[0]][0],
		plumbing.NewHash("fb72698cab7617ac416264415f13224dfd7a165e"))

}

func (suite *RwApiTestSuite) TestPushEmptyBranch() {
	t := suite.T()

	repoName := "empty-branch"
	orgSlug := "yandex"
	slug := repoName
	cloneURL, repoID := suite.emptyRepo(orgSlug, slug, suite.users.Admin)
	tmpDir := testutils.TempDir(t, "", "emptybranch")
	repoPath := path.Join(tmpDir, repoName)

	// clone repo
	cg := cgit.NewCGit(tmpDir).WithAuthToken(testutils.FakeIAMAuthToken(testutils.UserIdentities.Admin))
	cg.Must(t, "clone", cloneURL)
	cg = cgit.NewCGit(repoPath).WithAuthToken(testutils.FakeIAMAuthToken(testutils.UserIdentities.Admin))

	// push file to main first (can`t push empty branch to empty repo)
	logging.Info(nil, cg.Must(t, "checkout", "-b", "main"))
	logging.Info(nil, cg.Must(t, "config", "user.email", "you@example.com"))
	logging.Info(nil, cg.Must(t, "config", "user.name", "Your Name"))

	_, err := os.Create(path.Join(repoPath, "file.txt"))
	require.NoError(t, err)
	logging.Info(nil, cg.Must(t, "add", "."))
	logging.Info(nil, cg.Must(t, "commit", "-m", "initial"))
	logging.Info(nil, cg.Must(t, "push", "-u", "origin", "main"))

	// push one empty branch
	logging.Info(nil, cg.Must(t, "checkout", "-b", "br1"))
	logging.Info(nil, cg.Must(t, "push", "-u", "origin", "br1"))

	// push another empty branch
	logging.Info(nil, cg.Must(t, "checkout", "-b", "br2"))
	logging.Info(nil, cg.Must(t, "push", "-u", "origin", "br2"))

	t.Run("branches", func(t *testing.T) {
		client := pb.NewRepoServiceClient(suite.grpcClient)
		ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)

		resp, err := client.ListBranches(ctx, &pb.ListBranchesRequest{
			Id: grpc_marshalling.IDInverse(repoID),
		})
		yarequire.ProtoStatusEqual(t, codes.OK, err)

		//yarequire.ProtoDumpFixture(t, resp)
		yarequire.ProtoCompareWithFixture(t, resp,
			protocmp.IgnoreFields(&pb.Commit{}, "hash", "parent_commits"),
			protocmp.IgnoreFields(&pb.Signature{}, "date"),
		)
	})
}

func (suite *RwApiTestSuite) TestReceivePackCRUD() {
	t := suite.T()

	repoName := "receive-pack"
	orgSlug := "yandex"
	slug := repoName
	cloneURL, repoID := suite.emptyRepo(orgSlug, slug, suite.users.Admin)
	tmpDir := testutils.TempDir(t, "", "receivepack")
	repoPath := path.Join(tmpDir, repoName)

	// clone repo
	cg := cgit.NewCGit(tmpDir).WithAuthToken(testutils.FakeIAMAuthToken(testutils.UserIdentities.Admin))
	cg.Must(t, "clone", cloneURL)
	cg = cgit.NewCGit(repoPath).WithAuthToken(testutils.FakeIAMAuthToken(testutils.UserIdentities.Admin))

	// push file to main first (can`t push empty branch to empty repo)
	logging.Info(nil, cg.Must(t, "checkout", "-b", "main"))
	logging.Info(nil, cg.Must(t, "config", "user.email", "you@example.com"))
	logging.Info(nil, cg.Must(t, "config", "user.name", "Your Name"))

	_, err := os.Create(path.Join(repoPath, "file.txt"))
	require.NoError(t, err)
	logging.Info(nil, cg.Must(t, "add", "."))
	logging.Info(nil, cg.Must(t, "commit", "-m", "initial"))
	logging.Info(nil, cg.Must(t, "push", "-u", "origin", "main"))

	// create branches
	logging.Info(nil, cg.Must(t, "checkout", "-b", "br1"))
	logging.Info(nil, cg.Must(t, "checkout", "-b", "br2"))
	logging.Info(nil, cg.Must(t, "push", "-u", "origin", "br1", "br2"))

	// delete branch
	logging.Info(nil, cg.Must(t, "push", "-u", "origin", ":br1"))

	// update branch
	logging.Info(nil, cg.Must(t, "checkout", "br2"))
	_, err = os.Create(path.Join(repoPath, "file2.txt"))
	require.NoError(t, err)
	logging.Info(nil, cg.Must(t, "add", "."))
	logging.Info(nil, cg.Must(t, "commit", "-m", "second"))
	logging.Info(nil, cg.Must(t, "push", "-u", "origin", "br2"))

	t.Run("branches", func(t *testing.T) {
		client := pb.NewRepoServiceClient(suite.grpcClient)
		ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)

		resp, err := client.ListBranches(ctx, &pb.ListBranchesRequest{
			Id: grpc_marshalling.IDInverse(repoID),
		})
		yarequire.ProtoStatusEqual(t, codes.OK, err)

		// yarequire.ProtoDumpFixture(t, resp)
		yarequire.ProtoCompareWithFixture(t, resp,
			protocmp.IgnoreFields(&pb.Commit{}, "hash", "parent_commits"),
			protocmp.IgnoreFields(&pb.Signature{}, "date"),
		)
	})
}

func (suite *RwApiTestSuite) TestThinPackSupport() {
	t := suite.T()

	repoName := "thinpack"
	orgSlug := "yandex"
	slug := repoName
	cloneURL, _ := suite.emptyRepo(orgSlug, slug, suite.users.Admin)

	var gitClients []cgit.CGit
	for i := 0; i < 2; i++ {
		tmpDir := testutils.TempDir(t, "", "thinpack")
		repoPath := path.Join(tmpDir, repoName)

		// clone repo
		cg := cgit.NewCGit(tmpDir).WithAuthToken(testutils.FakeIAMAuthToken(testutils.UserIdentities.Admin))
		cg.Must(t, "clone", cloneURL)
		cg = cgit.NewCGit(repoPath).WithAuthToken(testutils.FakeIAMAuthToken(testutils.UserIdentities.Admin))

		// push file to main first (can`t push empty branch to empty repo)
		logging.Info(nil, cg.Must(t, "checkout", "-b", "main"))
		logging.Info(nil, cg.Must(t, "config", "user.email", fmt.Sprintf("client%d@example.com", i)))
		logging.Info(nil, cg.Must(t, "config", "user.name", fmt.Sprintf("Client%d", i)))

		gitClients = append(gitClients, cg)
	}

	text := strings.Repeat("Text line\n", 1000)
	fpath := path.Join(gitClients[0].Path(), "file.txt")
	err := os.WriteFile(fpath, []byte(text), 0665)
	require.NoError(t, err)
	logging.Info(nil, gitClients[0].Must(t, "add", "."))
	logging.Info(nil, gitClients[0].Must(t, "commit", "-m", "initial"))
	logging.Info(nil, gitClients[0].Must(t, "push", "-u", "origin", "main"))

	logging.Info(nil, gitClients[1].Must(t, "pull", "origin", "main"))

	f, err := os.OpenFile(fpath, os.O_APPEND|os.O_WRONLY, 0644)
	require.NoError(t, err)
	_, err = f.WriteString("Another line\n")
	require.NoError(t, err)
	err = f.Close()
	require.NoError(t, err)

	logging.Info(nil, gitClients[0].Must(t, "add", "."))
	logging.Info(nil, gitClients[0].Must(t, "commit", "-m", "initial"))
	logging.Info(nil, gitClients[0].Must(t, "push", "-u", "origin", "main"))

	logging.Info(nil, gitClients[1].Must(t, "pull", "origin", "main"))
}

func (suite *RwApiTestSuite) TestPushPrivate() {
	suite.checkPushWithVisibility(suite.T(), entities.Visibilities.Private)
}

func (suite *RwApiTestSuite) TestPushPublic() {
	suite.checkPushWithVisibility(suite.T(), entities.Visibilities.Public)
}

func (suite *RwApiTestSuite) TestPushInternal() {
	suite.checkPushWithVisibility(suite.T(), entities.Visibilities.Internal)
}

func (suite *RwApiTestSuite) checkPushWithVisibility(t *testing.T, vis entities.Visibility) {
	ctx := context.Background()

	tmpDir := testutils.TempDir(t, "", "basic")
	repoURL := suite.URL(suite.repos.Alpha)
	cgAdmin := cgit.NewCGit(tmpDir).WithAuthToken(testutils.FakeIAMAuthToken(testutils.UserIdentities.Kopatych))
	cgAdmin.Must(t, "clone", repoURL, "alpha")
	alphaDir := path.Join(tmpDir, "alpha")

	err := suite.RepoRepo.UpdateRepositoryByID(suite.repos.Alpha.ID).
		SetRepoVisibility(vis).
		Commit(ctx)
	require.NoError(t, err)

	t.Run(string(vis)+" repo: anonymous deny", func(t *testing.T) {
		cg := cgit.NewCGit(alphaDir)
		_, err := os.Create(path.Join(alphaDir, "newfile_anonymous_deny.txt"))
		require.NoError(t, err)
		logging.Info(nil, cg.Must(t, "add", "."))
		logging.Info(nil, cg.Must(t, "commit", "-m", "\"add newfile_anonymous_deny.txt\""))
		_, _, err = cg.Exec("push", "origin")
		require.Error(t, err)
	})

	t.Run(string(vis)+" repo: other users deny", func(t *testing.T) {
		if vis == entities.Visibilities.Public {
			t.Skip("all authentificated users are contributors for public repository")
		}
		cg := cgit.NewCGit(alphaDir).
			WithAuthToken(testutils.FakeIAMAuthToken(testutils.UserIdentities.Pikachu))
		_, err := os.Create(path.Join(alphaDir, "newfile_other_users_deny\".txt"))
		require.NoError(t, err)
		logging.Info(nil, cg.Must(t, "add", "."))
		logging.Info(nil, cg.Must(t, "commit", "-m", "\"add newfile_other_users_deny.txt\""))
		_, _, err = cg.Exec("push", "origin")
		require.Error(t, err)
	})

	t.Run(string(vis)+" repo: contributor disallow", func(t *testing.T) {
		kopatych, err := suite.UserRepo.GetUser(ctx, testutils.UserIdentities.Kopatych)
		require.NoError(t, err)

		err = suite.AccessBindingsService.CreateBindings(ctx, access.NullAuthenticator, []*entities.AccessBinding{
			{Subject: kopatych.Subject(), Object: suite.repos.Alpha.Object(), Role: iam.Roles.RepositoriesContributor},
		})
		require.NoError(t, err)

		cg := cgit.NewCGit(alphaDir).
			WithAuthToken(testutils.FakeIAMAuthToken(testutils.UserIdentities.Kopatych))
		_, err = os.Create(path.Join(alphaDir, "newfile_contributor_allow.txt"))
		require.NoError(t, err)
		logging.Info(nil, cg.Must(t, "add", "."))
		logging.Info(nil, cg.Must(t, "commit", "-m", "\"add newfile_contributor_allow.txt\""))
		_, _, err = cg.Exec("push", "origin")
		require.Error(t, err)
	})

	t.Run(string(vis)+" repo: viewer deny", func(t *testing.T) {
		if vis == entities.Visibilities.Public {
			t.Skip("all authentificated users are contributors for public repository")
		}
		krosh, err := suite.UserRepo.GetUser(ctx, testutils.UserIdentities.Krosh)
		require.NoError(t, err)

		err = suite.AccessBindingsService.CreateBindings(ctx, access.NullAuthenticator, []*entities.AccessBinding{
			{Subject: krosh.Subject(), Object: suite.repos.Alpha.Object(), Role: iam.Roles.RepositoriesViewer},
		})
		require.NoError(t, err)

		cg := cgit.NewCGit(alphaDir).
			WithAuthToken(testutils.FakeIAMAuthToken(krosh.Identity))
		_, err = os.Create(path.Join(alphaDir, "newfile_viewer_deny.txt"))
		require.NoError(t, err)
		logging.Info(nil, cg.Must(t, "add", "."))
		logging.Info(nil, cg.Must(t, "commit", "-m", "\"add newfile_viewer_deny.txt\""))
		_, _, err = cg.Exec("push", "origin")
		require.Error(t, err)
	})
}

func (suite *RwApiTestSuite) TestDeleteBranches() {
	t := suite.T()
	tmpDir := testutils.TempDir(t, "", "basic")
	repoURL := suite.URL(suite.repos.Alpha)
	cgAdmin := cgit.NewCGit(tmpDir).WithAuthToken(testutils.StubIAMToken)
	cgAdmin.Must(t, "clone", repoURL, "alpha")
	alphaDir := path.Join(tmpDir, "alpha")
	userIdentity := testutils.UserIdentities.Krosh
	user, err := suite.UserRepo.GetUser(context.Background(), userIdentity)
	require.NoError(t, err)
	err = suite.AccessBindingsService.CreateBindings(context.Background(), access.NullAuthenticator, []*entities.AccessBinding{
		{Subject: user.Subject(), Object: suite.repos.Alpha.Object(), Role: iam.Roles.RepositoriesMaintainer},
	})
	require.NoError(t, err)
	testcases := []struct {
		name       string
		wantErr    error
		branchList []string
	}{
		{
			name:       "master",
			wantErr:    except.CantDeleteDefaultBranch.Build(),
			branchList: []string{"master"},
		},
		{
			name:       "master and other",
			branchList: []string{"master", "branch"},
			wantErr:    except.CantDeleteDefaultBranch.Build(),
		},
		{
			name:       "other",
			branchList: []string{"keep-me"},
		},
	}
	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			cg := cgit.NewCGit(alphaDir).
				WithAuthToken(testutils.FakeIAMAuthToken(user.Identity))
			args := []string{"push", "origin", "--delete"}
			args = append(args, tc.branchList...)
			_, stderr, err := cg.Exec(args...)
			logging.Debug(context.Background(), "git stderr: ", stderr)
			if tc.wantErr == nil {
				require.NoError(t, err)
			} else {
				require.Error(t, err)

				require.Contains(t, stderr, tc.wantErr.Error())
			}
		})
	}
}

func (suite *RwApiTestSuite) TestPushNoDefaultBranch() {
	t := suite.T()

	repoName := "receive-pack"
	orgSlug := "yandex"
	slug := repoName
	cloneURL, _ := suite.emptyRepo(orgSlug, slug, suite.users.Admin)
	tmpDir := testutils.TempDir(t, "", "receivepack")
	repoPath := path.Join(tmpDir, repoName)

	// clone repo
	cg := cgit.NewCGit(tmpDir).WithAuthToken(testutils.FakeIAMAuthToken(testutils.UserIdentities.Admin))
	cg.Must(t, "clone", cloneURL)
	cg = cgit.NewCGit(repoPath).WithAuthToken(testutils.FakeIAMAuthToken(testutils.UserIdentities.Admin))

	logging.Info(nil, cg.Must(t, "checkout", "-b", "test"))
	logging.Info(nil, cg.Must(t, "config", "user.email", "you@example.com"))
	logging.Info(nil, cg.Must(t, "config", "user.name", "Your Name"))

	_, err := os.Create(path.Join(repoPath, "file.txt"))
	require.NoError(t, err)
	logging.Info(nil, cg.Must(t, "add", "."))
	logging.Info(nil, cg.Must(t, "commit", "-m", "initial"))
	logging.Info(nil, cg.Must(t, "push", "-u", "origin", "test"))

	anotherTmpDir := testutils.TempDir(t, "", "receivepackagain")

	// clone repo again
	cg = cgit.NewCGit(anotherTmpDir).WithAuthToken(testutils.FakeIAMAuthToken(testutils.UserIdentities.Admin))
	_, log := cg.MustAndLog(t, "clone", cloneURL)
	require.NotContains(t, log, "warning: remote HEAD refers to nonexistent ref, unable to checkout")
}

func (suite *RepoApiTestSuite) TestPushQuota() {
	t := suite.T()
	ctx := context.Background()

	orgSlug, repoSlug := uuid.NewString(), "push-quota"
	org := suite.OrganizationFixture(0, orgSlug, suite.users.Kopatych, nil)
	_, repoID := suite.emptyRepo(orgSlug, repoSlug, suite.users.Kopatych)

	suite.repoQuotaChecker.DisableBypass()
	defer suite.repoQuotaChecker.EnableBypass()

	tmpDir := testutils.TempDir(t, "", "push-quota")
	w := testutils.NewWorkdir(t, tmpDir)

	protocol := suite.SSHProtocol()
	cg := protocol.PrepareCGit(w, testutils.UserIdentities.Kopatych)

	cg.Must(t, "init", ".")
	cg.Must(t, "branch", "-m", "master")
	cg.Must(t, "remote", "add", "origin", protocol.RepoURL(orgSlug, repoSlug))

	t.Run("ok", func(t *testing.T) {
		packIDs, err := suite.StorageBackend.GetAllPackFileIDsS3(ctx, repoID)
		require.NoError(t, err)
		cntPacks := len(packIDs)

		cancel := suite.setQuotaLimit(t, org.ID, entities.Quotas.ObjectStorageSize, math.MaxInt32)
		defer cancel()

		q, err := suite.quotaService.Get(ctx, org.ID, entities.Quotas.ObjectStorageSize)
		require.NoError(t, err)
		require.Zero(t, q.Usage)

		suite.makeNewFile(cg, "test.txt")
		cg.Must(t, "add", "*")
		cg.Must(t, "commit", "-m", "cmt")

		_, _, err = cg.Exec("push", "-u", "origin", "master")
		require.NoError(t, err)

		q, err = suite.quotaService.Get(ctx, org.ID, entities.Quotas.ObjectStorageSize)
		require.NoError(t, err)
		require.NotZero(t, q.Usage)

		packIDs, err = suite.StorageBackend.GetAllPackFileIDsS3(ctx, repoID)
		require.NoError(t, err)
		require.Len(t, packIDs, cntPacks+1)
	})

	t.Run("limit", func(t *testing.T) {
		packIDs, err := suite.StorageBackend.GetAllPackFileIDsS3(ctx, repoID)
		require.NoError(t, err)
		cntPacks := len(packIDs)

		cancel := suite.setQuotaLimit(t, org.ID, entities.Quotas.ObjectStorageSize, 0)
		defer cancel()

		quotaInitial, err := suite.quotaService.Get(ctx, org.ID, entities.Quotas.ObjectStorageSize)
		require.NoError(t, err)

		suite.makeNewFile(cg, "test1.txt")
		cg.Must(t, "add", "*")
		cg.Must(t, "commit", "-m", "cmt")

		_, stderr, err := cg.Exec("push", "-u", "origin", "master")
		require.Error(t, err)
		require.Contains(t, stderr, except.QuotaLimitExceeded.Build(entities.Quotas.ObjectStorageSize, quotaInitial.Limit, quotaInitial.Usage).Error())

		q, err := suite.quotaService.Get(ctx, org.ID, entities.Quotas.ObjectStorageSize)
		require.NoError(t, err)
		require.Equal(t, quotaInitial, q)

		packIDs, err = suite.StorageBackend.GetAllPackFileIDsS3(ctx, repoID)
		require.NoError(t, err)
		require.Len(t, packIDs, cntPacks)
	})

	t.Run("limit_packfile", func(t *testing.T) {
		packIDs, err := suite.StorageBackend.GetAllPackFileIDsS3(ctx, repoID)
		require.NoError(t, err)
		cntPacks := len(packIDs)

		cancel := suite.setQuotaLimit(t, org.ID, entities.Quotas.PackFileSize, 10)
		defer cancel()

		suite.makeNewFile(cg, "test0.txt")
		cg.Must(t, "add", "*")
		cg.Must(t, "commit", "-m", "cmt")

		_, stderr, err := cg.Exec("push", "-u", "origin", "master")
		require.Error(t, err)
		require.Contains(t, stderr, except.QuotaLimitExceeded.Build(entities.Quotas.PackFileSize, float64(10), float64(0)).Error())

		packIDs, err = suite.StorageBackend.GetAllPackFileIDsS3(ctx, repoID)
		require.NoError(t, err)
		require.Len(t, packIDs, cntPacks)
	})
}

func (suite *RepoApiTestSuite) TestSignalOnRefUpdateToIDE() {
	t := suite.T()
	ctx := context.Background()

	s := signals.NewSignal(signals.SignalFlowTypes.Diagnostic)
	s.SetSignalID("deadbabe")

	err := suite.ideService.OnRefsUpdate(ctx, &signals.RefsUpdateDetails{
		SignalPayload:  s,
		OrgID:          suite.repos.Alpha.OrgID,
		RepoID:         suite.repos.Alpha.ID,
		RepoVisibility: entities.Visibilities.Public,
	})
	require.NoError(t, err)
}

func (suite *RepoApiTestSuite) TestSignalOnRefUpdateToCI() {
	t := suite.T()
	ctx := context.Background()

	s := signals.NewSignal(signals.SignalFlowTypes.Diagnostic)
	s.SetSignalID("deadbabe")

	err := suite.ciService.OnRefsUpdate(ctx, &signals.RefsUpdateDetails{
		SignalPayload: s,
		RefUpdates: []entities.RefUpdate{{
			Ref:     "master",
			Op:      entities.RefOperations.Deleted,
			OldHash: plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"),
			NewHash: plumbing.ZeroHash,
		}},
		SignalCallerPayload: signals.SignalCallerPayload{
			UserID: 0,
			Authenticator: common.NewIAMTokenAuthenticator(
				suite.cfg.ServiceAccounts.Robot.PrivateKey,
				&testutils.UserIdentities.Robot,
			).MarshalToStruct(),
		},
		OrgID:          suite.repos.Alpha.OrgID,
		RepoID:         suite.repos.Alpha.ID,
		RepoVisibility: entities.Visibilities.Public,
	})
	require.NoError(t, err)
}

func (suite *RepoApiTestSuite) TestRecalculateQuotaUsage() {
	t := suite.T()
	ctx := context.Background()

	task := housekeeping.NewQuotaUsageCmd(housekeeping.QuotaUsageDI{
		OrganizationRepository: suite.OrgRepo,
		RepositoryRepository:   suite.RepoRepo,
		LFSRepoFactory:         suite.LFSRepoFactory,
		QuotaService:           suite.quotaService,
		QuotaCalculator:        suite.quotaCalculator,
	})

	org := suite.orgs.Yandex
	quotaID := entities.Quotas.ObjectStorageSize
	privQuotaID := entities.Quotas.ObjectStoragePrivateSize

	t.Run("no change correct", func(t *testing.T) {
		qBefore, err := suite.quotaService.Get(ctx, org.ID, quotaID)
		require.NoError(t, err)

		err = task.Invoke(
			"-org_id", strconv.FormatUint(org.ID, 10),
			"-quota_id", string(quotaID),
			"-dry_run=false", "true",
		)
		require.NoError(t, err)

		qAfter, err := suite.quotaService.Get(ctx, org.ID, quotaID)
		require.NoError(t, err)

		require.Equal(t, qBefore, qAfter)
	})

	// change quota
	diff := int64(-100)
	privDiff := int64(100)
	err := suite.quotaService.IncrementUsage(ctx, org.ID, quotaID, diff)
	require.NoError(t, err)
	err = suite.quotaService.IncrementUsage(ctx, org.ID, privQuotaID, privDiff)
	require.NoError(t, err)

	t.Run("dry run", func(t *testing.T) {
		qBefore, err := suite.quotaService.Get(ctx, org.ID, quotaID)
		require.NoError(t, err)
		privQBefore, err := suite.quotaService.Get(ctx, org.ID, privQuotaID)
		require.NoError(t, err)

		err = task.Invoke(
			"-org_id", strconv.FormatUint(org.ID, 10),
			"-all_quotas", "true",
			"-dry_run", "true",
		)
		require.NoError(t, err)

		qAfter, err := suite.quotaService.Get(ctx, org.ID, quotaID)
		require.NoError(t, err)
		require.Equal(t, qBefore, qAfter)

		privQAfter, err := suite.quotaService.Get(ctx, org.ID, privQuotaID)
		require.NoError(t, err)
		require.Equal(t, privQBefore, privQAfter)
	})

	t.Run("recalculate", func(t *testing.T) {
		qBefore, err := suite.quotaService.Get(ctx, org.ID, quotaID)
		require.NoError(t, err)
		privQBefore, err := suite.quotaService.Get(ctx, org.ID, privQuotaID)
		require.NoError(t, err)

		err = task.Invoke(
			"--org_id", strconv.FormatUint(org.ID, 10),
			"--all_quotas",
			"--dry_run=0",
		)
		require.NoError(t, err)

		qAfter, err := suite.quotaService.Get(ctx, org.ID, quotaID)
		require.NoError(t, err)
		require.Equal(t, qBefore.Usage-float64(diff), qAfter.Usage)

		privQAfter, err := suite.quotaService.Get(ctx, org.ID, privQuotaID)
		require.NoError(t, err)
		require.Equal(t, privQBefore.Usage-float64(privDiff), privQAfter.Usage)

		calculated, err := suite.quotaCalculator.ObjectsSize(ctx, org.ID, entities.Visibilities.Public)
		require.NoError(t, err)
		require.EqualValues(t, calculated, qAfter.Usage)

		privCalculated, err := suite.quotaCalculator.ObjectsSize(ctx, org.ID, entities.Visibilities.Private)
		require.NoError(t, err)
		require.EqualValues(t, privCalculated, privQAfter.Usage)

		// run again, should remain same
		err = task.Invoke(
			"--org_id", strconv.FormatUint(org.ID, 10),
			"--all_quotas",
			"--dry_run=0",
		)
		require.NoError(t, err)

		qAfterCheck, err := suite.quotaService.Get(ctx, org.ID, quotaID)
		require.NoError(t, err)
		require.Equal(t, qAfterCheck.Usage, qAfter.Usage)

		privQAfterCheck, err := suite.quotaService.Get(ctx, org.ID, privQuotaID)
		require.NoError(t, err)
		require.Equal(t, privQAfterCheck.Usage, privQAfter.Usage)

		calculated, err = suite.quotaCalculator.ObjectsSize(ctx, org.ID, entities.Visibilities.Public)
		require.NoError(t, err)
		require.EqualValues(t, calculated, qAfter.Usage)

		privCalculated, err = suite.quotaCalculator.ObjectsSize(ctx, org.ID, entities.Visibilities.Private)
		require.NoError(t, err)
		require.EqualValues(t, privCalculated, privQAfter.Usage)
	})
}

func (suite *RwApiTestSuite) TestQuotaPrivOrg() {
	t := suite.T()
	ctx := context.Background()

	task := housekeeping.NewQuotaUsageCmd(housekeeping.QuotaUsageDI{
		OrganizationRepository: suite.OrgRepo,
		RepositoryRepository:   suite.RepoRepo,
		LFSRepoFactory:         suite.LFSRepoFactory,
		QuotaService:           suite.quotaService,
		QuotaCalculator:        suite.quotaCalculator,
	})

	org := suite.orgs.Yandex
	quotaID := entities.Quotas.ObjectStorageSize
	privQuotaID := entities.Quotas.ObjectStoragePrivateSize

	diff := int64(-100)
	privDiff := int64(100)

	err := suite.OrgRepo.UpdateOrganizationByID(org.ID).SetVisibility(entities.Visibilities.Private).Commit(ctx)
	require.NoError(t, err)

	qBefore, err := suite.quotaService.Get(ctx, org.ID, quotaID)
	require.NoError(t, err)
	privQBefore, err := suite.quotaService.Get(ctx, org.ID, privQuotaID)
	require.NoError(t, err)
	calculatedBefore, err := suite.quotaCalculator.ObjectsSize(ctx, org.ID, entities.Visibilities.Public)
	require.NoError(t, err)
	privCalculatedBefore, err := suite.quotaCalculator.ObjectsSize(ctx, org.ID, entities.Visibilities.Private)
	require.NoError(t, err)

	// test that housekeeping won't change anything
	err = task.Invoke(
		"--org_id", strconv.FormatUint(org.ID, 10),
		"--all_quotas",
		"--dry_run=0",
	)
	require.NoError(t, err)

	qAfter, err := suite.quotaService.Get(ctx, org.ID, quotaID)
	require.NoError(t, err)
	require.Equal(t, qBefore.Usage, qAfter.Usage)
	privQAfter, err := suite.quotaService.Get(ctx, org.ID, privQuotaID)
	require.NoError(t, err)
	require.Equal(t, privQBefore.Usage, privQAfter.Usage)
	calculatedAfter, err := suite.quotaCalculator.ObjectsSize(ctx, org.ID, entities.Visibilities.Public)
	require.NoError(t, err)
	require.Equal(t, calculatedBefore, calculatedAfter)
	privCalculatedAfter, err := suite.quotaCalculator.ObjectsSize(ctx, org.ID, entities.Visibilities.Private)
	require.NoError(t, err)
	require.Equal(t, privCalculatedBefore, privCalculatedAfter)

	// mess up usage in db
	err = suite.quotaService.IncrementUsage(ctx, org.ID, quotaID, diff)
	require.NoError(t, err)
	err = suite.quotaService.IncrementUsage(ctx, org.ID, privQuotaID, privDiff)
	require.NoError(t, err)

	err = task.Invoke(
		"--org_id", strconv.FormatUint(org.ID, 10),
		"--all_quotas",
		"--dry_run=0",
	)
	require.NoError(t, err)

	// check that everything is restored
	qAfter, err = suite.quotaService.Get(ctx, org.ID, quotaID)
	require.NoError(t, err)
	require.Equal(t, qBefore.Usage, qAfter.Usage)
	privQAfter, err = suite.quotaService.Get(ctx, org.ID, privQuotaID)
	require.NoError(t, err)
	require.Equal(t, privQBefore.Usage, privQAfter.Usage)
	calculatedAfter, err = suite.quotaCalculator.ObjectsSize(ctx, org.ID, entities.Visibilities.Public)
	require.NoError(t, err)
	require.Equal(t, calculatedBefore, calculatedAfter)
	privCalculatedAfter, err = suite.quotaCalculator.ObjectsSize(ctx, org.ID, entities.Visibilities.Private)
	require.NoError(t, err)
	require.Equal(t, privCalculatedBefore, privCalculatedAfter)
}
