package integrationtests

import (
	"common/cgit"
	"fmt"
	"gitcore/internal/testutils"
	"os"
	"path"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func (suite *RepoApiTestSuite) TestConcurrentPush() {
	t := suite.T()

	const parallel = 4

	for _, protocol := range []Protocol{suite.HTTPSProtocol(), suite.SSHProtocol()} {
		t.Run(protocol.Name, func(t *testing.T) {
			_, orgSlug, repoSlug := suite.makeRandomRepo(t, suite.users.Kopatych)

			repoURL := protocol.RepoURL(orgSlug, repoSlug)

			tmpDir := testutils.TempDir(t, "", "concurrent")
			w := testutils.NewWorkdir(t, tmpDir)

			cg := protocol.PrepareCGit(w, testutils.UserIdentities.Kopatych)

			cg.Must(t, "init", ".")
			cg.Must(t, "remote", "add", "origin", repoURL)
			require.NoError(t, os.WriteFile(path.Join(w.Path(), "README"), []byte("1"), 0777))
			cg.Must(t, "checkout", "-b", "mmm")
			cg.Must(t, "add", "*")
			cg.Must(t, "commit", "-m", "initial")
			cg.Must(t, "push", "-u", "origin", "mmm")

			var cgs []cgit.CGit
			for i := 0; i < parallel; i++ {
				tmpDir := testutils.TempDir(t, "", "concurrent")
				w := testutils.NewWorkdir(t, tmpDir)

				cg := protocol.PrepareCGit(w, testutils.UserIdentities.Kopatych)
				cg.Must(t, "clone", repoURL, ".")

				cgs = append(cgs, cg)

				log, err := cg.GetLog("mmm")
				require.NoError(t, err)
				require.Len(t, log, 1)

				require.NoError(t, os.WriteFile(path.Join(w.Path(), "file.txt"),
					[]byte(strings.Repeat(fmt.Sprintf("text %d", i), 100)), 0777))

				cg.Must(t, "add", "file.txt")
				cg.Must(t, "commit", "-m", fmt.Sprintf("second (parallel %d)", i))
			}

			var succeeded []int
			var errs []error
			var wg sync.WaitGroup
			var mtx sync.Mutex
			wg.Add(len(cgs))
			for i := 0; i < len(cgs); i++ {
				go func(cg *cgit.CGit, num int) {
					_, _, err := cg.Exec("push")

					mtx.Lock()
					defer mtx.Unlock()
					if err != nil {
						errs = append(errs, err)
					} else {
						succeeded = append(succeeded, num)
					}
					wg.Done()
				}(&cgs[i], i)
			}
			wg.Wait()

			cg.Must(t, "pull")

			log, err := cg.GetLog("mmm")
			require.NoError(t, err)

			require.Greater(t, len(log), 1, "at least one push must be accepted")
			require.Equal(t, 1+parallel-len(errs), len(log),
				"all pushes that not fail should appear in the log, succeded: %+v", succeeded)
		})
	}
}
