package integrationtests

import (
	"common/cgit"
	"common/logging"
	"common/utils"
	"common/utils/rolesgenerator/iam"
	"context"
	"fmt"
	"gitcore/internal/entities"
	"gitcore/internal/git/server"
	"gitcore/internal/services/access"
	ssherrors "gitcore/internal/sshserver/errors"
	"gitcore/internal/testutils"
	"github.com/go-git/go-git/v5/plumbing"
	"os"
	"path"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func (suite *RwApiTestSuite) TestCloneNonExistingBranch() {
	t := suite.T()
	repoURL := suite.SSHProtocol().RepoURL(suite.repos.Alpha.OrgSlug, suite.repos.Alpha.Slug)

	tmpDir := testutils.TempDir(t, "", "testrepo22")
	w := testutils.NewWorkdir(t, tmpDir)
	cg := suite.SSHProtocol().PrepareCGit(w, testutils.UserIdentities.Kopatych)

	_, _, err := cg.Exec("clone", "-b", "xxxx", repoURL)
	require.Error(t, err)
}

func (suite *RwApiTestSuite) TestCloneSingleBranch() {
	t := suite.T()

	suite.addRole(t, suite.users.Kopatych, suite.repos.Alpha, iam.Roles.RepositoriesDeveloper)

	slug := "yandex/alpha"
	repoURL := fmt.Sprintf("%s%s.git", suite.gitHost, slug)

	tmpDir := testutils.TempDir(t, "", "basic")
	cg := cgit.NewCGit(tmpDir).WithAuthToken(testutils.FakeIAMAuthToken(testutils.UserIdentities.Barash))
	cg.Trace = true
	cg.TracePacket = true
	cg.VerboseCurl = true
	_, errs, err := cg.Exec("clone", repoURL)
	require.NoError(t, err)
	require.Contains(t, errs, "Recv header: Content-Security-Policy: default-src 'none'; base-uri 'none'; form-action 'none'; sandbox;")
	require.Contains(t, errs, "Recv header: X-Frame-Options: DENY")

	cg = cgit.NewCGit(path.Join(tmpDir, "alpha")).WithAuthToken(testutils.FakeIAMAuthToken(testutils.UserIdentities.Barash))

	tmpDir2 := testutils.TempDir(t, "", "basic")
	cg2 := cgit.NewCGit(tmpDir2).WithAuthToken(testutils.FakeIAMAuthToken(testutils.UserIdentities.Kopatych))
	cg2.Must(t, "clone", repoURL)
	cg2 = cgit.NewCGit(path.Join(tmpDir2, "alpha")).WithAuthToken(testutils.FakeIAMAuthToken(testutils.UserIdentities.Kopatych))

	cg2.Must(t, "checkout", "-b", "new_branch")
	_, err = os.Create(path.Join(tmpDir2, "alpha", "newfile.txt"))
	require.NoError(t, err)
	readme, err := os.OpenFile(path.Join(tmpDir2, "alpha", ".gitignore"), os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
	require.NoError(t, err)
	_, err = readme.WriteString("some_build_stuff\n")
	require.NoError(t, err)
	require.NoError(t, readme.Close())
	require.NoError(t, os.Remove(path.Join(tmpDir2, "alpha", "binary.jpg")))
	cg2.Must(t, "add", ".")
	cg2.Must(t, "commit", "-m", "\"add newfile.txt\"")

	cg2.Must(t, "push", "-u", "origin", "new_branch")

	cg.Must(t, "branch", "-v", "-a")
	cg.Must(t, "fetch")
	cg.Must(t, "branch", "-v", "-a")
}

func (suite *RwApiTestSuite) TestClonePrivateRepo() {
	t := suite.T()
	ctx := context.Background()

	err := suite.RepoRepo.UpdateRepositoryByID(suite.repos.Alpha.ID).
		SetRepoVisibility(entities.Visibilities.Private).
		Commit(ctx)
	require.NoError(t, err)
	repoURL := suite.URL(suite.repos.Alpha)

	t.Run("anonymous deny", func(t *testing.T) {
		tmpDir := testutils.TempDir(t, "", "basic")
		cg := cgit.NewCGit(tmpDir)

		stdout, stderr, err := cg.Exec("clone", repoURL)
		logging.Info(ctx, stdout)
		logging.Debug(ctx, stderr)
		require.Error(t, err)
	})

	t.Run("user with role – allow", func(t *testing.T) {
		kopatych, err := suite.UserRepo.GetUser(ctx, testutils.UserIdentities.Kopatych)
		require.NoError(t, err)

		err = suite.AccessBindingsService.CreateBindings(ctx, access.StubAuthenticator, []*entities.AccessBinding{
			{Subject: kopatych.Subject(), Object: suite.repos.Alpha.Object(), Role: iam.Roles.RepositoriesViewer},
		})
		require.NoError(t, err)

		tmpDir := testutils.TempDir(t, "", "basic")
		cg := cgit.NewCGit(tmpDir).
			WithAuthToken(testutils.FakeIAMAuthToken(testutils.UserIdentities.Kopatych))
		cg.Must(t, "clone", repoURL)
	})

	t.Run("pat auth – allow", func(t *testing.T) {
		user, err := suite.UserRepo.GetUser(ctx, testutils.UserIdentities.Pikachu)
		require.NoError(t, err)

		err = suite.AccessBindingsService.CreateBindings(ctx, access.StubAuthenticator, []*entities.AccessBinding{
			{Subject: user.Subject(), Object: suite.repos.Alpha.Object(), Role: iam.Roles.RepositoriesViewer},
		})
		require.NoError(t, err)

		_, token := suite.createSimplePAT(t, user)

		tmpDir := testutils.TempDir(t, "", "basic")
		cg := cgit.NewCGit(tmpDir).
			WithAuthToken(token)
		cg.Must(t, "clone", repoURL)
	})

	t.Run("pat auth – eternal", func(t *testing.T) {
		user, err := suite.UserRepo.GetUser(ctx, testutils.UserIdentities.Pikachu)
		require.NoError(t, err)

		err = suite.AccessBindingsService.CreateBindings(ctx, access.StubAuthenticator, []*entities.AccessBinding{
			{Subject: user.Subject(), Object: suite.repos.Alpha.Object(), Role: iam.Roles.RepositoriesViewer},
		})
		require.NoError(t, err)

		_, token := suite.createEternalPAT(t, user)

		tmpDir := testutils.TempDir(t, "", "basic")
		cg := cgit.NewCGit(tmpDir).
			WithAuthToken(token)
		logging.Info(nil, cg.Must(t, "clone", repoURL))
	})

	t.Run("pat expired – deny", func(t *testing.T) {
		user, err := suite.UserRepo.GetUser(ctx, testutils.UserIdentities.Raichu)
		require.NoError(t, err)

		err = suite.AccessBindingsService.CreateBindings(ctx, access.StubAuthenticator, []*entities.AccessBinding{
			{Subject: user.Subject(), Object: suite.repos.Alpha.Object(), Role: iam.Roles.RepositoriesViewer},
		})
		require.NoError(t, err)

		_, token := suite.createPATWithExpiration(t, user, utils.PtrFromValue(time.Now().Add(time.Millisecond*300)))
		time.Sleep(time.Millisecond * 500)

		tmpDir := testutils.TempDir(t, "", "basic")
		cg := cgit.NewCGit(tmpDir).
			WithAuthToken(token)
		stdout, stderr, err := cg.Exec("clone", repoURL)
		logging.Info(ctx, stdout)
		logging.Debug(ctx, stderr)
		require.Error(t, err)
	})

	t.Run("other users deny", func(t *testing.T) {
		tmpDir := testutils.TempDir(t, "", "basic")
		cg := cgit.NewCGit(tmpDir).
			WithAuthToken(testutils.FakeIAMAuthToken(testutils.UserIdentities.Barash))

		stdout, stderr, err := cg.Exec("clone", repoURL)
		logging.Info(ctx, stdout)
		logging.Debug(ctx, stderr)
		require.Error(t, err)
	})
}

func (suite *RwApiTestSuite) TestClonePrivateRepoSSH() {
	t := suite.T()
	ctx := context.Background()

	err := suite.RepoRepo.UpdateRepositoryByID(suite.repos.Alpha.ID).
		SetRepoVisibility(entities.Visibilities.Private).
		Commit(ctx)
	require.NoError(t, err)
	suite.addRole(t, suite.users.Barash, suite.repos.Alpha, iam.Roles.Admin)

	protocol := suite.SSHProtocol()
	repoURL := protocol.RepoURL(suite.repos.Alpha.OrgSlug, suite.repos.Alpha.Slug)

	var tests = []struct {
		name         string
		userIdentity entities.UserIdentity
		wantError    bool
	}{
		{
			name:         "admin",
			userIdentity: testutils.UserIdentities.Barash,
			wantError:    false,
		},
		{
			name:         "forbidden",
			userIdentity: testutils.UserIdentities.Slowpoke,
			wantError:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			tmpDir := testutils.TempDir(t, "", "")
			w := testutils.NewWorkdir(t, tmpDir)

			cg := protocol.PrepareCGit(w, tt.userIdentity)

			_, stderr, err := cg.Exec("clone", repoURL)
			if tt.wantError {
				require.Contains(t, stderr, ssherrors.ErrPermissionDenied.Error())
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
		})
	}
}

func (suite *RwApiTestSuite) TestCloneWithIncludeTag() {
	t := suite.T()
	ctx := context.Background()

	// Get test repository
	testRepo := suite.repos.ListTags
	testUser := suite.users.Krosh
	token := testutils.FakeIAMAuthToken(testUser.Identity)

	suite.addRole(t, testUser, testRepo, iam.Roles.RepositoriesDeveloper)
	repoURL := fmt.Sprintf("%s%s.git", suite.gitHost, testRepo.FullSlug())
	repoURLSSH := fmt.Sprintf("%s%s.git", suite.sshHost, testRepo.FullSlug())

	// Clone the repo and populate with more commits, branches, and tags
	tmpDir1 := testutils.TempDir(t, "", "populate")
	cg1 := cgit.NewCGit(tmpDir1).WithAuthToken(token)
	cg1.Must(t, "clone", repoURL)

	repoDir := path.Join(tmpDir1, testRepo.Slug)
	cg1 = cgit.NewCGit(repoDir).WithAuthToken(token)

	// Create additional commits on master branch
	_, err := os.Create(path.Join(repoDir, "file1.txt"))
	require.NoError(t, err)
	cg1.Must(t, "add", "file1.txt")
	cg1.Must(t, "commit", "-m", "first commit")

	cg1.Must(t, "commit", "-m", "first commit-2", "--allow-empty")
	cg1.Must(t, "commit", "-m", "first commit-3", "--allow-empty")

	output, err := cg1.ExecOutput(ctx, "rev-parse", "HEAD")
	require.NoError(t, err)
	shallowCommit := plumbing.NewHash(strings.TrimSpace(output))

	cg1.Must(t, "commit", "-m", "first commit-4", "--allow-empty")

	_, err = os.Create(path.Join(repoDir, "file2.txt"))
	require.NoError(t, err)
	cg1.Must(t, "add", "file2.txt")
	cg1.Must(t, "commit", "-m", "second commit")

	// Create annotated tags on main branch
	cg1.Must(t, "tag", "-a", "v1.0.0", "-m", "version 1.0.0", "HEAD~1")
	cg1.Must(t, "tag", "-a", "v1.1.0", "-m", "version 1.1.0", "HEAD")

	// Create a feature branch with its own commits and tag
	cg1.Must(t, "checkout", "-b", "feature-branch")
	_, err = os.Create(path.Join(repoDir, "feature.txt"))
	require.NoError(t, err)
	cg1.Must(t, "add", "feature.txt")
	cg1.Must(t, "commit", "-m", "feature commit")
	cg1.Must(t, "tag", "-a", "feature-tag", "-m", "feature tag", "HEAD")

	// Switch back to main and push everything
	cg1.Must(t, "checkout", "main")
	cg1.Must(t, "push", "origin", "--mirror")

	// Test 1: Clone with --single-branch (should include tags from main via IncludeTag)
	t.Run("include_tags", func(t *testing.T) {
		tmpDir2 := testutils.TempDir(t, "", "single-branch")
		cg2 := cgit.NewCGit(tmpDir2).WithAuthToken(token)
		cg2.Must(t, "clone", "--single-branch", repoURL)

		clonedRepo := path.Join(tmpDir2, testRepo.Slug)
		cg2 = cgit.NewCGit(clonedRepo).WithAuthToken(token)

		// Check that main branch exists
		branches, _, err := cg2.Exec("branch", "-r")
		require.NoError(t, err)
		require.Contains(t, branches, "origin/main")
		require.NotContains(t, branches, "origin/feature-branch", "feature-branch should not be cloned with --single-branch")

		// Check that tags from main branch are included via IncludeTag capability
		tags, _, err := cg2.Exec("tag", "-l")
		require.NoError(t, err)
		require.Contains(t, tags, "v1.0.0")
		require.Contains(t, tags, "v1.1.0")
		require.NotContains(t, tags, "feature-tag", "feature-tag should not be included (its commit is not in master)")
	})

	// Test 2: Clone without --single-branch (should include all tags)
	t.Run("all_tags", func(t *testing.T) {
		tmpDir3 := testutils.TempDir(t, "", "all-branches")
		cg3 := cgit.NewCGit(tmpDir3).WithAuthToken(token)
		cg3.Must(t, "clone", repoURL)

		clonedRepo := path.Join(tmpDir3, testRepo.Slug)
		cg3 = cgit.NewCGit(clonedRepo).WithAuthToken(token)

		// Check that all branches exist
		branches, _, err := cg3.Exec("branch", "-r")
		require.NoError(t, err)
		require.Contains(t, branches, "origin/main")
		require.Contains(t, branches, "origin/feature-branch")

		// Check that all tags are included
		tags, _, err := cg3.Exec("tag", "-l")
		require.NoError(t, err)
		require.Contains(t, tags, "v1.0.0")
		require.Contains(t, tags, "v1.1.0")
		require.Contains(t, tags, "feature-tag", "feature-tag should be included when all branches are cloned")
	})

	// Test 3: Clone with --single-branch using protocol v2 (should include tags via IncludeTag)
	t.Run("include_tags_v2", func(t *testing.T) {
		tmpDir4 := testutils.TempDir(t, "", "single-branch-v2")
		cg4 := cgit.NewCGit(tmpDir4).WithAuthToken(token)

		// Enable Git protocol v2
		cg4.Protocol = cgit.Version2
		cg4.AddExtraHeader(server.DevV2HeaderName, "true")

		cg4.Must(t, "clone", "--single-branch", repoURL)

		clonedRepo := path.Join(tmpDir4, testRepo.Slug)
		cg4 = cgit.NewCGit(clonedRepo).WithAuthToken(token)

		// Check that main branch exists
		branches, _, err := cg4.Exec("branch", "-r")
		require.NoError(t, err)
		require.Contains(t, branches, "origin/main")
		require.NotContains(t, branches, "origin/feature-branch", "feature-branch should not be cloned with --single-branch")

		// Check that tags from main branch are included via IncludeTag capability in protocol v2
		tags, _, err := cg4.Exec("tag", "-l")
		require.NoError(t, err)
		require.Contains(t, tags, "v1.0.0")
		require.Contains(t, tags, "v1.1.0")
		require.NotContains(t, tags, "feature-tag")
	})

	// Test 3: Clone with shallow-exclude
	t.Run("clone with shallow-exclude set to hash", func(t *testing.T) {
		for _, useV2 := range []bool{false, true} {
			t.Run("with v2 protocol="+strconv.FormatBool(useV2), func(t *testing.T) {
				tmpDir4 := testutils.TempDir(t, "", "single-branch-v2")
				cg4 := cgit.NewCGit(tmpDir4).WithAuthToken(token)

				// Enable Git protocol v2
				if useV2 {
					cg4.Protocol = cgit.Version2
					cg4.AddExtraHeader(server.DevV2HeaderName, "true")
				}

				cg4.AddExtraHeader(server.AllowSHAInDeepenNotHeaderName, "1")

				cg4.Must(t, "clone", "--single-branch", repoURL, "--shallow-exclude="+shallowCommit.String())
			})

			t.Run("ssh with v2 protocol="+strconv.FormatBool(useV2), func(t *testing.T) {
				tmpDir4 := testutils.TempDir(t, "", "single-branch-v2")
				cg4 := cgit.NewCGit(tmpDir4).WithSSHKeyPath(
					suite.GetSSHKey(KnownSSHKeyTypes()[0], testUser.Identity))

				// Enable Git protocol v2
				if useV2 {
					cg4.Protocol = cgit.Version2
					cg4.SSHEnvVars = append(cg4.SSHEnvVars, cgit.EnvVar{Name: server.DevV2EnvName, Value: "true"})
				}

				cg4.SSHEnvVars = append(cg4.SSHEnvVars, cgit.EnvVar{Name: server.AllowSHAInDeepenNotEnvName, Value: "1"})

				cg4.Must(t, "clone", "--single-branch", repoURLSSH, "--shallow-exclude="+shallowCommit.String())
			})
		}
	})
}
