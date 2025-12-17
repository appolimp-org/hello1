package integrationtests

import (
	"bytes"
	"fmt"
	"gitcore/internal/testutils"
	"net"
	"regexp"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"github.com/stretchr/testify/require"
	gossh "golang.org/x/crypto/ssh"
)

func (suite *RepoApiTestSuite) TestSidebandIsReported() {
	t := suite.T()

	sidebandTest := func(t *testing.T, proto Protocol) {
		orgSlug := "sidebandorg" + proto.Name
		repoSlug := "sidebandrepo" + proto.Name
		suite.OrganizationFixture(0, orgSlug, suite.users.Kopatych, nil)
		suite.emptyRepo(orgSlug, repoSlug, suite.users.Kopatych)

		repoURL := fmt.Sprintf("%s%s/%s.git", proto.Host, orgSlug, repoSlug)

		tmpDir := testutils.TempDir(t, "", "sideband")

		w := testutils.NewWorkdir(t, tmpDir)
		gitCLI := proto.PrepareCGit(w, testutils.UserIdentities.Kopatych)
		gitCLI.Must(t, "init")
		gitCLI.Must(t, "checkout", "-b", "master")
		w.MkFile("file1.txt", "foo")
		gitCLI.Must(t, "add", "file1.txt")
		gitCLI.Must(t, "commit", "-m", "new commit")
		gitCLI.Must(t, "remote", "add", "origin", repoURL)
		_, pushErr := gitCLI.MustAndLog(t, "push", "-u", "origin", "master")
		require.Contains(t, pushErr, "Updating references")

		tmpDir3 := testutils.TempDir(t, "", "gogit1")
		w2 := testutils.NewWorkdir(t, tmpDir3)
		gitCLI2 := proto.PrepareCGit(w2, testutils.UserIdentities.Kopatych)
		gitCLI2.Must(t, "clone", repoURL)

		tmpDir2 := testutils.TempDir(t, "", "gogit2")
		outb := &bytes.Buffer{}

		sshAuth, err := ssh.NewPublicKeysFromFile("git",
			suite.GetSSHKey(KnownSSHKeyTypes()[0], testutils.UserIdentities.Kopatych), "")
		sshAuth.HostKeyCallback = func(hostname string, remote net.Addr, key gossh.PublicKey) error {
			return nil
		}
		require.NoError(t, err)

		_, err = git.PlainClone(tmpDir2, false, &git.CloneOptions{
			URL:           fmt.Sprintf("%s%s/%s.git", suite.SSHProtocol().Host, orgSlug, repoSlug),
			Progress:      outb, // Saving progress
			Auth:          sshAuth,
			ReferenceName: "master",
		})

		require.NoError(t, err)

		// Check that we've reported about packing objects
		re := regexp.MustCompile(`\r\n|\r|\n`)
		lines := re.Split(outb.String(), -1)
		require.GreaterOrEqual(t, len(lines), 3, "At least three progress events must come")
		for _, l := range lines {
			if len(l) > 0 {
				if strings.HasPrefix(l, "Total") {
					require.Regexp(t, "^Total \\d* \\(delta \\d*\\), reused \\d* \\(delta \\d*\\), pack-reused \\d* \\(from \\d*\\)$", l)
				} else {
					require.Regexp(t, "^(Enumerating objects)|(Counting objects)|(Packing objects): \\d* of \\d*\\s*.*", l)
				}
			}
		}
	}

	t.Run("Testing sideband for protocol SSH", func(t *testing.T) {
		sidebandTest(t, suite.SSHProtocol())
	})

	t.Run("Testing sideband for protocol HTTPS", func(t *testing.T) {
		sidebandTest(t, suite.HTTPSProtocol())
	})
}
