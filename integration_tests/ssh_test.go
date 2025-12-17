package integrationtests

import (
	"common/cgit"
	"common/utils/rolesgenerator/iam"
	"gitcore/internal/testutils"
	"strings"
	"testing"

	"github.com/fatih/color"
	"github.com/stretchr/testify/require"
)

func (suite *RepoApiTestSuite) TestAllSshKeys() {
	t := suite.T()

	tmpDir := testutils.TempDir(t, "", "empty")

	w := testutils.NewWorkdir(t, tmpDir)

	for _, k := range KnownSSHKeyTypes() {
		t.Run("Cloning with "+k+" ssh key", func(t *testing.T) {
			gitCLI := w.CGit().WithSSHKeyPath(suite.GetSSHKey(k, testutils.UserIdentities.Kopatych))
			gitCLI.Must(t, "clone", suite.GetSSHRepoURL("yandex", "alpha"), k)
		})
	}
}

func (suite *RepoApiTestSuite) TestSshSmoke() {
	t := suite.T()

	sshKey := suite.GetSSHKey(KnownSSHKeyTypes()[0], testutils.UserIdentities.Kopatych)
	remotePath := suite.GetSSHRepoURL("yandex", "alpha")
	sshKeyDev := suite.GetSSHKey(KnownSSHKeyTypes()[0], testutils.UserIdentities.Barash)
	suite.addRole(t, suite.users.Barash, suite.repos.Alpha, iam.Roles.RepositoriesDeveloper)

	color.NoColor = false
	color.Cyan("Smoke test for %s\n\n", remotePath)

	var w, validator *testutils.Workdir
	var gitCLI, gitCLI2 cgit.CGit

	tmpDir := testutils.TempDir(t, "", "smoke")
	tmpDir2 := testutils.TempDir(t, "", "smokeClone")

	connectionClosedErrs := []string{
		"Connection closed by remote host",
		"Connection closed by ::1 port 2222",
		"Connection reset by ::1 port 2222",
		"Connection reset by 127.0.0.1 port 2222",
	}
	requireConnectionClosedErr := func(t *testing.T, errMsg string) {
		for _, option := range connectionClosedErrs {
			if strings.Contains(errMsg, option) {
				return // ok
			}
		}
		t.Fatalf("error message '%s' doesn't contain any specific connection closed signal message", errMsg)
	}

	t.Run("No anon login", func(t *testing.T) {
		w = testutils.NewWorkdir(t, tmpDir)
		validator = testutils.NewWorkdir(t, tmpDir2)
		otherDir := t.TempDir()

		gitCLI = w.CGit()

		for i := 0; i < suite.cfg.SSH.Fail2Ban.Attempts; i++ {
			_, stdErr, err := gitCLI.Exec("clone", remotePath, otherDir)
			require.NotNil(t, err)
			require.Contains(t, stdErr, "Permission denied")
		}

		_, stdErr, err := gitCLI.Exec("clone", remotePath, otherDir)
		require.NotNil(t, err)
		requireConnectionClosedErr(t, stdErr)
	})

	suite.Params.Fail2Ban.UnbanAll()

	t.Run("Clone empty repos", func(t *testing.T) {
		w = testutils.NewWorkdir(t, tmpDir)
		validator = testutils.NewWorkdir(t, tmpDir2)

		gitCLI = w.CGit().WithSSHKeyPath(sshKey)
		gitCLI2 = validator.CGit().WithSSHKeyPath(sshKey)

		gitCLI.Must(t, "clone", remotePath, ".")
		gitCLI2.Must(t, "clone", remotePath, ".")
	})

	t.Run("Initial commit", func(t *testing.T) {
		w.MkFile("1.txt", "foo")
		w.MkFile("2.txt", "bar")
		w.MkFile("foo/bar/3.txt", "baz")

		gitCLI = w.CGit().WithSSHKeyPath(sshKeyDev)
		gitCLI2 = validator.CGit().WithSSHKeyPath(sshKey)

		gitCLI.Must(t, "add", ".")
		gitCLI.Must(t, "commit", "-am", "\"initial commit\"")
		gitCLI.Must(t, "push")

		// validation

		gitCLI2.Must(t, "pull")

		validator.CheckFile("1.txt", "foo")
		validator.CheckFile("2.txt", "bar")
		validator.CheckFile("foo/bar/3.txt", "baz")
	})
	t.Run("Edit files and commit", func(t *testing.T) {

		w.MkFile("1.txt", "foo*edited")
		w.MkFile("foo/bar/4.txt", "xyz")
		w.RmFile("2.txt")

		gitCLI = w.CGit().WithSSHKeyPath(sshKeyDev)
		gitCLI2 = validator.CGit().WithSSHKeyPath(sshKey)

		gitCLI.Must(t, "add", ".")
		gitCLI.Must(t, "commit", "-am", "\"commit2\"")
		gitCLI.Must(t, "push")

		gitCLI2.Must(t, "pull")

		// validation

		validator.CheckFile("1.txt", "foo*edited")
		validator.MustBeMissing("2.txt")
		validator.CheckFile("foo/bar/3.txt", "baz")
		validator.CheckFile("foo/bar/4.txt", "xyz")
	})
	t.Run("Branch", func(t *testing.T) {
		gitCLI = w.CGit().WithSSHKeyPath(sshKeyDev)
		gitCLI2 = validator.CGit().WithSSHKeyPath(sshKey)

		gitCLI.Must(t, "checkout", "-b", "feature/1")
		w.MkFile("feature.file", "test")
		gitCLI.Must(t, "add", ".")
		gitCLI.Must(t, "commit", "-am", "\"feature/1\"")
		gitCLI.Must(t, "push", "--set-upstream", "origin", "feature/1")

		// validation
		gitCLI2.Must(t, "fetch", "--all")
		gitCLI2.Must(t, "checkout", "feature/1")
		validator.CheckFile("feature.file", "test")
	})
}
