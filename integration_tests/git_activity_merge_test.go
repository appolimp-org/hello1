package integrationtests

import (
	"common/cgit"
	"common/oyaml"
	"context"
	pullrequest_errors "gitcore/internal/pullrequests/errors"
	"gitcore/internal/pullrequests/merge_v2"
	"gitcore/internal/testutils"
	"github.com/cockroachdb/errors"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"strconv"
	"strings"
	"testing"
	"time"
)

const commitMessage = "commitMessage"

func (suite *RwApiTestSuite) TestGitOperationActivity_MergeStrategies() {
	t := suite.T()

	testCases := []struct {
		name                              string
		mergeMethod                       merge_v2.MergeMethod
		setupConflict                     bool
		setupNothingToCommit              bool
		setupFork                         bool
		setupThinpack                     bool
		setupBranchPolicy                 bool
		setupBranchPolicyAllowPR          bool
		setupBranchPolicyPreventForcePush bool
		setupLFS                          bool
		wantErrorType                     string
		verifyCommits                     func(t *testing.T, cg cgit.CGit)
	}{
		{
			name:        "MergeCommit",
			mergeMethod: merge_v2.MergeMethods.Merge,
			verifyCommits: func(t *testing.T, cg cgit.CGit) {
				logOutput, err := cg.ExecOutput(context.Background(), "log", "--oneline", "-n", "1", "master")
				require.NoError(t, err)
				require.Contains(t, logOutput, commitMessage)

				// Verify merge commit has 2 parents
				parents, err := cg.ExecOutput(context.Background(), "rev-list", "--parents", "-n", "1", "master")
				require.NoError(t, err)
				parentsList := strings.Fields(strings.TrimSpace(parents))
				require.Equal(t, 3, len(parentsList), "Merge commit should have 2 parents (3 hashes total including commit itself)")
			},
		},
		{
			name:        "SquashMerge",
			mergeMethod: merge_v2.MergeMethods.Squash,
			verifyCommits: func(t *testing.T, cg cgit.CGit) {
				logOutput, err := cg.ExecOutput(context.Background(), "log", "--oneline", "-n", "1", "master")
				require.NoError(t, err)
				require.Contains(t, logOutput, commitMessage)

				// Verify squash commit has only 1 parent
				parents, err := cg.ExecOutput(context.Background(), "rev-list", "--parents", "-n", "1", "master")
				require.NoError(t, err)
				parentsList := strings.Fields(strings.TrimSpace(parents))
				require.Equal(t, 2, len(parentsList), "Squash commit should have 1 parent (2 hashes total including commit itself)")
			},
		},
		{
			name:        "RebaseMerge",
			mergeMethod: merge_v2.MergeMethods.Rebase,
			verifyCommits: func(t *testing.T, cg cgit.CGit) {
				logOutput, err := cg.ExecOutput(context.Background(), "log", "--oneline", "-n", "3", "master")
				require.NoError(t, err)
				require.Contains(t, logOutput, "Feature commit 2")
				require.Contains(t, logOutput, "Feature commit 1")

				// Verify linear history (all commits have 1 parent)
				parents, err := cg.ExecOutput(context.Background(), "rev-list", "--parents", "-n", "3", "master")
				require.NoError(t, err)
				lines := strings.Split(strings.TrimSpace(parents), "\n")
				for _, line := range lines {
					parentsList := strings.Fields(line)
					require.True(t, len(parentsList) <= 2, "Rebase should create linear history (max 1 parent per commit)")
				}
			},
		},
		{
			name:          "MergeWithConflict",
			setupConflict: true,
			wantErrorType: pullrequest_errors.ErrCodeConflict,
		},
		{
			name:                 "NothingToCommit",
			setupNothingToCommit: true,
			wantErrorType:        pullrequest_errors.ErrCodeNothingToCommit,
		},
		{
			name:      "ForkMerge",
			setupFork: true,
			verifyCommits: func(t *testing.T, cg cgit.CGit) {
				logOutput, err := cg.ExecOutput(context.Background(), "log", "--oneline", "-n", "1", "master")
				require.NoError(t, err)
				require.Contains(t, logOutput, commitMessage)

				// Verify fork merge commit has 2 parents
				parents, err := cg.ExecOutput(context.Background(), "rev-list", "--parents", "-n", "1", "master")
				require.NoError(t, err)
				parentsList := strings.Fields(strings.TrimSpace(parents))
				require.Equal(t, 3, len(parentsList), "Fork merge commit should have 2 parents (3 hashes total including commit itself)")
			},
		},
		{
			name:          "ThinpackMerge",
			setupThinpack: true,
			verifyCommits: func(t *testing.T, cg cgit.CGit) {
				logOutput, err := cg.ExecOutput(context.Background(), "log", "--oneline", "-n", "1", "master")
				require.NoError(t, err)
				require.Contains(t, logOutput, commitMessage)

				// Verify thinpack merge commit has 2 parents
				parents, err := cg.ExecOutput(context.Background(), "rev-list", "--parents", "-n", "1", "master")
				require.NoError(t, err)
				parentsList := strings.Fields(strings.TrimSpace(parents))
				require.Equal(t, 3, len(parentsList), "Thinpack merge commit should have 2 parents (3 hashes total including commit itself)")

				// Verify that all large files exist in the merged result
				_, err = cg.ExecOutput(context.Background(), "ls-files", "large_file1.bin")
				require.NoError(t, err, "large_file1.bin should exist after merge")
				_, err = cg.ExecOutput(context.Background(), "ls-files", "large_file2.bin")
				require.NoError(t, err, "large_file2.bin should exist after merge")
				_, err = cg.ExecOutput(context.Background(), "ls-files", "large_file3.bin")
				require.NoError(t, err, "large_file3.bin should exist after merge")
			},
		},
		{
			name:              "BranchPolicyReject",
			setupBranchPolicy: true,
			wantErrorType:     pullrequest_errors.ErrCodePushRejectedByBranchPolicy,
		},
		{
			name:                     "BranchPolicyAllowPRRebase",
			mergeMethod:              merge_v2.MergeMethods.Rebase,
			setupBranchPolicyAllowPR: true,
			verifyCommits: func(t *testing.T, cg cgit.CGit) {
				logOutput, err := cg.ExecOutput(context.Background(), "log", "--oneline", "-n", "2", "master")
				require.NoError(t, err)
				require.Contains(t, logOutput, "Feature commit 1")

				// Verify linear history (all commits have 1 parent)
				parents, err := cg.ExecOutput(context.Background(), "rev-list", "--parents", "-n", "2", "master")
				require.NoError(t, err)
				lines := strings.Split(strings.TrimSpace(parents), "\n")
				for _, line := range lines {
					parentsList := strings.Fields(line)
					require.True(t, len(parentsList) <= 2, "Rebase should create linear history (max 1 parent per commit)")
				}
			},
		},
		{
			name:                     "BranchPolicyAllowPRMerge",
			mergeMethod:              merge_v2.MergeMethods.Merge,
			setupBranchPolicyAllowPR: true,
			verifyCommits: func(t *testing.T, cg cgit.CGit) {
				logOutput, err := cg.ExecOutput(context.Background(), "log", "--oneline", "-n", "1", "master")
				require.NoError(t, err)
				require.Contains(t, logOutput, commitMessage)

				// Verify merge commit has 2 parents
				parents, err := cg.ExecOutput(context.Background(), "rev-list", "--parents", "-n", "1", "master")
				require.NoError(t, err)
				parentsList := strings.Fields(strings.TrimSpace(parents))
				require.Equal(t, 3, len(parentsList), "Merge commit should have 2 parents (3 hashes total including commit itself)")
			},
		},
		{
			name:                              "BranchPolicyPreventForcePushAllowPRRebase",
			mergeMethod:                       merge_v2.MergeMethods.Rebase,
			setupBranchPolicyPreventForcePush: true,
			verifyCommits: func(t *testing.T, cg cgit.CGit) {
				logOutput, err := cg.ExecOutput(context.Background(), "log", "--oneline", "-n", "2", "master")
				require.NoError(t, err)
				require.Contains(t, logOutput, "Feature commit 1")

				// Verify linear history (all commits have 1 parent)
				parents, err := cg.ExecOutput(context.Background(), "rev-list", "--parents", "-n", "2", "master")
				require.NoError(t, err)
				lines := strings.Split(strings.TrimSpace(parents), "\n")
				for _, line := range lines {
					parentsList := strings.Fields(line)
					require.True(t, len(parentsList) <= 2, "Rebase should create linear history (max 1 parent per commit)")
				}
			},
		},
		{
			name:     "MergeWithLFS",
			setupLFS: true,
			verifyCommits: func(t *testing.T, cg cgit.CGit) {
				logOutput, err := cg.ExecOutput(context.Background(), "log", "--oneline", "-n", "1", "master")
				require.NoError(t, err)
				require.Contains(t, logOutput, commitMessage)

				// Verify LFS file exists and is properly tracked
				_, err = cg.ExecOutput(context.Background(), "ls-files", "large_file.txt")
				require.NoError(t, err, "large_file.txt should exist after merge")

				// Verify merge commit has 2 parents
				parents, err := cg.ExecOutput(context.Background(), "rev-list", "--parents", "-n", "1", "master")
				require.NoError(t, err)
				parentsList := strings.Fields(strings.TrimSpace(parents))
				require.Equal(t, 3, len(parentsList), "Merge commit should have 2 parents (3 hashes total including commit itself)")
			},
		},
		{
			name:      "ForkMergeWithLFS",
			setupFork: true,
			setupLFS:  true,
			verifyCommits: func(t *testing.T, cg cgit.CGit) {
				logOutput, err := cg.ExecOutput(context.Background(), "log", "--oneline", "-n", "1", "master")
				require.NoError(t, err)
				require.Contains(t, logOutput, commitMessage)

				// Verify LFS file exists and is properly tracked
				_, err = cg.ExecOutput(context.Background(), "ls-files", "large_file.txt")
				require.NoError(t, err, "large_file.txt should exist after merge")
			},
		},
	}

	ct, err := time.Parse(time.RFC822Z, "22 Aug 05 21:18 +0200")
	require.NoError(t, err)

	for _, mwc := range []bool{false, true} {
		t.Run("mwc="+strconv.FormatBool(mwc), func(t *testing.T) {
			for _, tc := range testCases {
				t.Run(tc.name, func(t *testing.T) {
					targetRepoID, orgSlug, repoSlug := suite.makeRandomRepo(t, suite.users.Kopatych)
					sourceRepoID := targetRepoID

					protocol := suite.HTTPSProtocol()
					tmpDir := testutils.TempDir(t, "", "merge-test-"+tc.name)
					w := testutils.NewWorkdir(t, tmpDir)
					cg := protocol.PrepareCGit(w, testutils.UserIdentities.Kopatych)
					cg.CommitTime = ct

					// Setup repository with master and feature branch
					cg.Must(t, "init")
					cg.Must(t, "remote", "add", "origin", protocol.RepoURL(orgSlug, repoSlug))
					cg.Must(t, "checkout", "-b", "master")

					// Create initial commit on master
					w.MkFile("README.md", "# Initial commit")
					cg.Must(t, "add", "README.md")
					cg.Must(t, "commit", "-m", "Initial commit")
					cg.Must(t, "push", "-u", "origin", "master")

					// Create feature branch with multiple commits
					cg.Must(t, "checkout", "-b", "feature")

					if tc.setupConflict {
						// Create conflicting changes
						// Both branches will modify the same file
						w.MkFile("conflict.txt", "Feature version of file")
						cg.Must(t, "add", "conflict.txt")
						cg.Must(t, "commit", "-m", "Feature commit with conflict")
						cg.Must(t, "push", "-u", "origin", "feature")

						// Go back to master and create conflicting change
						cg.Must(t, "checkout", "master")
						w.MkFile("conflict.txt", "Master version of file")
						cg.Must(t, "add", "conflict.txt")
						cg.Must(t, "commit", "-m", "Master commit with conflict")
						cg.Must(t, "push", "origin", "master")
					} else if tc.setupNothingToCommit {
						// Create feature branch with some changes
						w.MkFile("feature.txt", "Feature content")
						cg.Must(t, "add", "feature.txt")
						cg.Must(t, "commit", "-m", "Feature commit")
						cg.Must(t, "push", "-u", "origin", "feature")

						// Go back to master and merge feature branch manually (simulate already merged)
						cg.Must(t, "checkout", "master")
						cg.Must(t, "merge", "feature", "--no-ff", "-m", "Already merged feature")
						cg.Must(t, "push", "origin", "master")

						// Now feature branch has nothing new to merge into master
					} else if tc.setupFork {
						var sourceOrgSlug, sourceRepoSlug string
						sourceRepoID, sourceOrgSlug, sourceRepoSlug = suite.makeRandomRepo(t, suite.users.Kopatych)

						// Fork setup: setup source repository
						if tc.setupLFS {
							cg = cg.WithAuthTokenSite(suite.RepoURL(sourceOrgSlug, sourceRepoSlug))
							cg.CommitTime = ct

							// Initialize LFS in the repo
							cg.Must(t, "lfs", "install")
							cg.Must(t, "lfs", "track", "*.txt")
							cg.Must(t, "add", ".gitattributes")
							cg.Must(t, "commit", "-m", "Setup LFS tracking")

							// Create a large file that will be LFS-tracked
							largeContent := make([]byte, 1024)
							for i := range largeContent {
								largeContent[i] = 'A' + byte(i%26)
							}
							w.MkFile("large_file.txt", string(largeContent))
							cg.Must(t, "add", "large_file.txt")
							cg.Must(t, "commit", "-m", "Add LFS file")
						} else {
							// First push to main repository
							w.MkFile("feature1.txt", "Feature file 1")
							cg.Must(t, "add", "feature1.txt")
							cg.Must(t, "commit", "-m", "Feature commit 1")

							w.MkFile("feature2.txt", "Feature file 2")
							cg.Must(t, "add", "feature2.txt")
							cg.Must(t, "commit", "-m", "Feature commit 2")
						}

						// Now setup the source repository (fork) by pushing feature branch to it
						cg.Must(t, "remote", "add", "fork", protocol.RepoURL(sourceOrgSlug, sourceRepoSlug))
						cg.Must(t, "push", "fork", "feature")
					} else if tc.setupThinpack {
						// Thinpack scenario: create identical large file in multiple commits
						// This triggers thinpack behavior where Git optimizes pack files by avoiding duplicate objects
						largeContent := "This is a large file content that will be duplicated to test thinpack behavior"

						// First commit with one large file
						w.MkFile("large_file1.bin", largeContent)
						cg.Must(t, "add", "large_file1.bin")
						cg.Must(t, "commit", "-m", "First commit with large file")
						cg.Must(t, "push", "-u", "origin", "feature")

						// Second commit with identical large file (same content)
						w.MkFile("large_file2.bin", largeContent) // Same content as large_file1.bin
						cg.Must(t, "add", "large_file2.bin")
						cg.Must(t, "commit", "-m", "Second commit with identical large file")
						cg.Must(t, "push", "origin", "feature")

						// Third commit with another identical large file (same content)
						w.MkFile("large_file3.bin", largeContent) // Same content as previous files
						cg.Must(t, "add", "large_file3.bin")
						cg.Must(t, "commit", "-m", "Third commit with identical large file")
						cg.Must(t, "push", "origin", "feature")
					} else if tc.setupBranchPolicy {
						// Branch policy scenario: setup branch protection that prevents all changes to master
						// First, create feature branch with some changes
						w.MkFile("feature1.txt", "Feature file 1")
						cg.Must(t, "add", "feature1.txt")
						cg.Must(t, "commit", "-m", "Feature commit 1")
						cg.Must(t, "push", "-u", "origin", "feature")

						// Go back to master and setup branch policy that prevents all changes
						cg.Must(t, "checkout", "master")
						w.MkFile(oyaml.BranchPolicyPath, `
branch_protection:
  policies:
    - target: branch
      matches: master
      message: "Master branch is protected"
      rules:
        - prevent_all_changes 
`)
						cg.Must(t, "add", oyaml.BranchPolicyPath)
						cg.Must(t, "commit", "-m", "add branch policy config")
						cg.Must(t, "push", "origin", "master")
					} else if tc.setupBranchPolicyAllowPR {
						// Branch policy scenario: setup branch protection that allows PR merges but prevents direct commits
						// First, create feature branch with some changes
						w.MkFile("feature1.txt", "Feature file 1")
						cg.Must(t, "add", "feature1.txt")
						cg.Must(t, "commit", "-m", "Feature commit 1")
						cg.Must(t, "push", "-u", "origin", "feature")

						// Go back to master and setup branch policy that prevents non-PR changes
						cg.Must(t, "checkout", "master")
						w.MkFile(oyaml.BranchPolicyPath, `
branch_protection:
  policies:
    - target: branch
      matches: master
      message: "Master branch only allows PR merges"
      rules:
        - prevent_non_pr_changes
`)
						cg.Must(t, "add", oyaml.BranchPolicyPath)
						cg.Must(t, "commit", "-m", "add branch policy config")
						cg.Must(t, "push", "origin", "master")
					} else if tc.setupBranchPolicyPreventForcePush {
						// Branch policy scenario: setup branch protection that prevents force push but allows PR merges
						// First, create feature branch with a commit
						w.MkFile("feature1.txt", "Feature file 1")
						cg.Must(t, "add", "feature1.txt")
						cg.Must(t, "commit", "-m", "Feature commit 1")
						cg.Must(t, "push", "-u", "origin", "feature")

						// Go back to master and create divergent commit (this will cause non-fast-forward on rebase)
						cg.Must(t, "checkout", "master")
						w.MkFile("master_file.txt", "Master file content")
						cg.Must(t, "add", "master_file.txt")
						cg.Must(t, "commit", "-m", "Master commit creating divergence")

						// Setup branch policy that prevents force push
						w.MkFile(oyaml.BranchPolicyPath, `
branch_protection:
  policies:
    - target: branch
      matches: master
      message: "Master branch prevents force push"
      rules:
        - prevent_force_push
`)
						cg.Must(t, "add", oyaml.BranchPolicyPath)
						cg.Must(t, "commit", "-m", "add branch policy config")
						cg.Must(t, "push", "origin", "master")
					} else if tc.setupLFS {
						// LFS scenario: create LFS tracked files
						// Initialize LFS in the repo
						cg = cg.WithAuthTokenSite(suite.RepoURL(orgSlug, repoSlug))
						cg.Must(t, "lfs", "install")
						cg.Must(t, "lfs", "track", "*.txt")
						cg.Must(t, "add", ".gitattributes")
						cg.Must(t, "commit", "-m", "Setup LFS tracking")

						// Create a large file that will be LFS-tracked
						largeContent := make([]byte, 1024)
						for i := range largeContent {
							largeContent[i] = 'A' + byte(i%26)
						}
						w.MkFile("large_file.txt", string(largeContent))
						cg.Must(t, "add", "large_file.txt")
						cg.Must(t, "commit", "-m", "Add LFS file")
						cg.Must(t, "push", "-u", "origin", "feature")
					} else {
						// Normal non-conflicting commits
						w.MkFile("feature1.txt", "Feature file 1")
						cg.Must(t, "add", "feature1.txt")
						cg.Must(t, "commit", "-m", "Feature commit 1")

						w.MkFile("feature2.txt", "Feature file 2")
						cg.Must(t, "add", "feature2.txt")
						cg.Must(t, "commit", "-m", "Feature commit 2")
						cg.Must(t, "push", "-u", "origin", "feature")
					}

					// Get source hash for feature branch
					featureHash, err := cg.ExecOutput(context.Background(), "rev-parse", "feature")
					require.NoError(t, err)
					sourceHash := plumbing.NewHash(strings.TrimSpace(featureHash))

					ccfg := *suite.cfg
					ccfg.Tasks.Merge.EnableCheckoutFreeMerge = mwc

					// Setup GitActivity
					activity := merge_v2.NewGitActivity(merge_v2.GitActivityDI{
						Config:                     &ccfg,
						StorageBackend:             suite.StorageBackend,
						GitFSFactory:               suite.GitFSFactory,
						CGitRepoFactory:            suite.CGitRepoFactory,
						PackCache:                  suite.PackCache,
						PackSessionFactory:         suite.PackSessionFactory,
						RepositoryRepository:       suite.RepoRepo,
						UserRepository:             suite.UserRepo,
						ReferenceRepositoryFactory: suite.RefRepoFactory,
						LFSObjectRepositoryFactory: suite.LFSRepoFactory,
						LfsObjectStorageFactory:    suite.LfsObjectStorageFactory,
						MergerV2Factory:            suite.MergerV2Factory,
					})

					testSuite := &testsuite.WorkflowTestSuite{}
					env := testSuite.NewTestActivityEnvironment()
					env.RegisterActivity(activity.MergeV2)

					var result *merge_v2.MergeResult
					encodedValue, err := env.ExecuteActivity(activity.MergeV2, merge_v2.MergeParam{
						PrepareResult: merge_v2.PrepareResult{
							TargetRepoID:  targetRepoID,
							TargetBranch:  "master",
							SourceRepoID:  sourceRepoID,
							SourceBranch:  "feature",
							SourceHash:    sourceHash,
							MergeMethod:   tc.mergeMethod,
							CommiterName:  "Test User",
							CommiterEmail: "test@example.com",
							CommitMessage: commitMessage,
						},
						Authenticator: *suite.getFakeAuthenticator(suite.users.Kopatych.Identity).MarshalToStruct(),
					})

					if tc.wantErrorType != "" {
						var appErr *temporal.ApplicationError
						require.True(t, errors.As(err, &appErr))
						require.Equal(t, tc.wantErrorType, appErr.Type())
						return
					}

					require.NoError(t, err)
					require.NoError(t, encodedValue.Get(&result))

					// Pull latest changes and verify results
					cg.Must(t, "checkout", "master")
					cg.Must(t, "pull", "origin", "master")

					// Verify final hash matches result
					masterHash, err := cg.ExecOutput(context.Background(), "rev-parse", "master")
					require.NoError(t, err)
					require.Equal(t, strings.TrimSpace(masterHash), result.MergeHash.String())

					if tc.verifyCommits != nil {
						tc.verifyCommits(t, cg)
					}
				})
			}
		})
	}
}
