package integrationtests

import (
	"common/grpc"
	"common/logging"
	"common/oyaml"
	"common/testutils/yarequire"
	yautils "common/utils"
	"common/utils/rolesgenerator/iam"
	"context"
	"gitcore/internal/entities"
	"gitcore/internal/testutils"
	"google.golang.org/grpc/codes"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func (suite *RwApiTestSuite) TestYamlSyntaxError() {
	for _, configPath := range []string{oyaml.BranchPolicyPath, oyaml.OldPath} {
		suite.BeforeTest("", "")

		t := suite.T()

		protocol := suite.HTTPSProtocol()
		_, orgSlug, repoSlug := suite.makeRandomRepo(t, suite.users.Kopatych)
		tmpDir := testutils.TempDir(t, "", "testrepo")
		w := testutils.NewWorkdir(t, tmpDir)
		cg := protocol.PrepareCGit(w, testutils.UserIdentities.Kopatych)

		cg.Must(t, "init", ".")
		cg.Must(t, "remote", "add", "origin", protocol.RepoURL(orgSlug, repoSlug))
		cg.Must(t, "branch", "-m", "master")

		// empty old config to check newer is preferred
		suite.makeNewFileWithContent(cg, oyaml.OldPath, "")
		suite.makeNewFileWithContent(cg, configPath, `
	branch_protection:
	policies:
		- target: branch
		matches: master
		message: "do not use force"
		rules:
			- prevent_force_push
			- this_rule_does_not_exists 
	`)
		suite.commitAll(cg, "add config", "master")

		t.Run("broken yaml must not fail the push", func(t *testing.T) {
			suite.makeNewFileWithContent(cg, "1.txt", "hello world")
			suite.commitAll(cg, "initial", "master")
		})

		suite.AfterTest("", "")
	}
}

func (suite *RwApiTestSuite) TestBranchPolicyBypass() {
	for _, configPath := range []string{oyaml.BranchPolicyPath, oyaml.OldPath} {
		suite.BeforeTest("", "")

		t := suite.T()

		protocol := suite.HTTPSProtocol()
		repoID, orgSlug, repoSlug := suite.makeRandomRepo(t, suite.users.Kopatych)
		repo, err := suite.RepoRepo.GetRepositoryByID(context.Background(), repoID)
		require.NoError(t, err)

		suite.addRole(t, suite.users.Krosh, repo, iam.Roles.RepositoriesDeveloper) // can not use bypass, but can commit

		tmpDir := testutils.TempDir(t, "", "testrepo")

		w := testutils.NewWorkdir(t, tmpDir)

		cgAdmin := protocol.PrepareCGit(w, testutils.UserIdentities.Kopatych)
		cgDeveloper := protocol.PrepareCGit(w, testutils.UserIdentities.Krosh)

		cgAdmin.Must(t, "init", ".")
		cgAdmin.Must(t, "remote", "add", "origin", protocol.RepoURL(orgSlug, repoSlug))
		cgAdmin.Must(t, "branch", "-m", "master")

		suite.makeNewFileWithContent(cgAdmin, oyaml.OldPath, "")
		suite.makeNewFileWithContent(cgAdmin, configPath, `
branch_protection:
  policies:
    - target: branch
      matches: master
      message: "oops I locked myself"
      rules:
        - prevent_all_changes 
`)
		suite.commitAll(cgAdmin, "add config", "master")

		suite.makeNewFileWithContent(cgAdmin, "1.txt", "hello world")
		cgAdmin.Must(t, "add", ".")
		cgAdmin.Must(t, "commit", "-am", "something new")

		t.Run("admin without bypass can not do anything", func(t *testing.T) {
			stdout, stderr, err := cgAdmin.Exec("push")

			logging.Info(context.Background(), "Stdout: %s, Stderr: %s", stdout, stderr)
			require.NotNil(t, err)
			require.Contains(t, stderr, "prevent_all_changes")
		})

		t.Run("bypass default", func(t *testing.T) {
			ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
			client := pb.NewBranchPolicyServiceClient(suite.grpcClient)

			now := time.Now()
			_, err = client.SetBypass(ctx, &pb.SetBypassRequest{
				RepoId: grpc.MarshalID(repoID),
				Value:  true,
			})
			require.NoError(t, err)

			res, err := client.GetBypass(ctx, &pb.GetBypassRequest{
				RepoId: grpc.MarshalID(repoID),
			})
			require.NoError(t, err)

			require.WithinDuration(t, res.GetDeadline().AsTime(), now.Add(15*time.Minute), 15*time.Second)
		})

		t.Run("bypass for a day", func(t *testing.T) {
			ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
			client := pb.NewBranchPolicyServiceClient(suite.grpcClient)

			_, err = client.SetBypass(ctx, &pb.SetBypassRequest{
				RepoId:          grpc.MarshalID(repoID),
				Value:           true,
				DurationMinutes: 1440,
			})

			yarequire.ProtoStatusEqual(t, codes.InvalidArgument, err)
		})

		t.Run("bypass", func(t *testing.T) {
			ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
			client := pb.NewBranchPolicyServiceClient(suite.grpcClient)

			now := time.Now()
			_, err = client.SetBypass(ctx, &pb.SetBypassRequest{
				RepoId:          grpc.MarshalID(repoID),
				Value:           true,
				DurationMinutes: 10,
			})
			require.NoError(t, err)

			res, err := client.GetBypass(ctx, &pb.GetBypassRequest{
				RepoId: grpc.MarshalID(repoID),
			})
			require.NoError(t, err)

			require.WithinDuration(t, res.GetDeadline().AsTime(), now.Add(10*time.Minute), 15*time.Second)

			defer func() {
				// cleanup
				_, err = suite.Params.BranchPolicyBypassService.SetBypass(ctx, repo.ID, false, 0)
				require.NoError(t, err)
			}()

			// developer still can not push, despite bypass is on:
			_, stderr, err := cgDeveloper.Exec("push")
			require.NotNil(t, err)
			require.Contains(t, stderr, "prevent_all_changes")

			// but Admin can push:
			_, _, err = cgAdmin.Exec("push")
			require.NoError(t, err)
		})

		suite.AfterTest("", "")
	}
}

func (suite *RwApiTestSuite) TestBranchPolicyCommit() {
	for _, configPath := range []string{oyaml.BranchPolicyPath, oyaml.OldPath} {
		suite.BeforeTest("", "")

		t := suite.T()
		protocol := suite.HTTPSProtocol()
		repoID, orgSlug, repoSlug := suite.makeRandomRepo(t, suite.users.Kopatych)
		repo, err := suite.RepoRepo.GetRepositoryByID(context.Background(), repoID)
		require.NoError(t, err)

		tmpDir := testutils.TempDir(t, "", "testrepo")

		w := testutils.NewWorkdir(t, tmpDir)

		cgAdmin := protocol.PrepareCGit(w, testutils.UserIdentities.Kopatych)

		cgAdmin.Must(t, "init", ".")
		cgAdmin.Must(t, "remote", "add", "origin", protocol.RepoURL(orgSlug, repoSlug))
		cgAdmin.Must(t, "branch", "-m", "master")

		ctx := context.Background()

		authenticator := suite.getFakeAuthenticator(suite.users.Kopatych.Identity)

		t.Run("check Commit is nil when remote repository was not init (no config && no init)", func(t *testing.T) {
			cfg, err := suite.Caas.GetBranchProtectionConfig(ctx, authenticator, entities.NewGitRevisionAsDefaultBranch(repo.ID))
			require.NoError(t, err)
			require.NotNil(t, cfg)
		})

		t.Run("check Commit is not nil when remote repository was init (no config && init)", func(t *testing.T) {
			suite.makeNewFileWithContent(cgAdmin, "README.txt", "hello")
			cgAdmin.Must(t, "add", "*")
			cgAdmin.Must(t, "commit", "-am", "init")
			cgAdmin.Must(t, "push", "-u", "origin", "master")

			cfg, err := suite.Caas.GetBranchProtectionConfig(ctx, authenticator, entities.NewGitRevisionAsDefaultBranch(repo.ID))
			require.NoError(t, err)
			require.NotNil(t, cfg)
		})

		t.Run("check Commit is not nil when remote repository has invalid config file (invalid config && init)", func(t *testing.T) {
			suite.makeNewFileWithContent(cgAdmin, oyaml.OldPath, "")
			suite.makeNewFileWithContent(cgAdmin, configPath, `
branch_protection:
  invalid_file: ???
`)
			cgAdmin.Must(t, "add", "*")
			cgAdmin.Must(t, "commit", "-am", "add config")
			cgAdmin.Must(t, "push", "-u", "origin", "master")

			cfg, err := suite.Caas.GetBranchProtectionConfig(ctx, authenticator, entities.NewGitRevisionAsDefaultBranch(repo.ID))
			require.Error(t, err)
			require.Nil(t, cfg)
		})

		t.Run("check Commit is not nil when remote repository has valid config file (config && init)", func(t *testing.T) {
			suite.makeNewFileWithContent(cgAdmin, oyaml.OldPath, "")
			suite.makeNewFileWithContent(cgAdmin, configPath, `
branch_protection:
  policies:
    - target: branch
      matches: master
      message: "do not use force"
      rules:
        - prevent_force_push
`)
			cgAdmin.Must(t, "add", "*")
			cgAdmin.Must(t, "commit", "-am", "add config")
			cgAdmin.Must(t, "push", "-u", "origin", "master")

			cfg, err := suite.Caas.GetBranchProtectionConfig(ctx, authenticator, entities.NewGitRevisionAsDefaultBranch(repo.ID))
			require.NoError(t, err)
			require.NotNil(t, cfg)
		})

		suite.AfterTest("", "")
	}
}

func (suite *RwApiTestSuite) TestBranchPolicy() {
	for _, configPath := range []string{oyaml.BranchPolicyPath, oyaml.OldPath} {
		suite.BeforeTest("", "")

		t := suite.T()

		protocol := suite.HTTPSProtocol()
		repoID, orgSlug, repoSlug := suite.makeRandomRepo(t, suite.users.Kopatych)

		tmpDir := testutils.TempDir(t, "", "testrepo")
		w := testutils.NewWorkdir(t, tmpDir)

		cg := protocol.PrepareCGit(w, testutils.UserIdentities.Kopatych)

		cg.Must(t, "init", ".")
		cg.Must(t, "remote", "add", "origin", protocol.RepoURL(orgSlug, repoSlug))

		cg.Must(t, "branch", "-m", "master")
		suite.makeNewFileWithContent(cg, "1.txt", "hello")
		suite.commitAll(cg, "initial", "master")

		cg.Must(t, "checkout", "-b", "legacy/feature1")
		suite.makeNewFileWithContent(cg, "2.txt", "hello")
		suite.commitAll(cg, "feature1", "legacy/feature1")

		cg.Must(t, "checkout", "-b", "nonpr", "master")
		suite.makeNewFileWithContent(cg, "onlypr", "hello")
		suite.commitAll(cg, "onlypr", "nonpr")

		cg.Must(t, "checkout", "master")
		suite.makeNewFileWithContent(cg, oyaml.OldPath, "")
		suite.makeNewFileWithContent(cg, configPath, `
branch_protection:
  policies:
    - target: default_branch
      message: "do not use force"
      rules:
        - prevent_force_push
    - target: branch
      matches: nonpr
      message: "non pr change"
      rules:
        - prevent_non_pr_changes
    - target: branch
      matches: releases/**
      message: "do not create"
      rules:
        - prevent_creation
    - target: tag
      matches: gitcore-* 
      message: "do not delete"
      rules:
        - prevent_deletion
    - target: branch
      matches: legacy/** 
      message: "any change to such branch is forbidden"
      rules:
        - prevent_all_changes 
    - target: branch
      matches: protected/*
      message: "only PR changes allowed, but can create"
      rules:
        - prevent_non_pr_changes
`)
		suite.commitAll(cg, "add config", "master")

		t.Run("no rewrite push - ok", func(t *testing.T) {
			suite.makeNewFileWithContent(cg, "1.txt", "hello world")
			suite.commitAll(cg, "initial", "master")
		})

		// each rule has unique message for us to catch:

		t.Run("check prevent_force_push", func(t *testing.T) {
			cg.Must(t, "commit", "--amend", "-m", "aaaa")
			stdout, stderr, err := cg.Exec("push", "origin", "--force")
			logging.Info(context.Background(), "Stdout: %s, Stderr: %s", stdout, stderr)
			require.NotNil(t, err)
			require.Contains(t, stderr, "prevent_force_push")
		})

		t.Run("check prevent_creation", func(t *testing.T) {
			cg.Must(t, "checkout", "-b", "releases/v0.0.1", "master")
			stdout, stderr, err := cg.Exec("push", "origin", "-u", "releases/v0.0.1")
			logging.Info(context.Background(), "Stdout: %s, Stderr: %s", stdout, stderr)
			require.NotNil(t, err)
			require.Contains(t, stderr, "prevent_creation")
		})

		t.Run("check prevent_deletion", func(t *testing.T) {
			cg.Must(t, "tag", "gitcore-0.0.1")
			cg.Must(t, "push", "origin", "--tags")

			stdout, stderr, err := cg.Exec("push", "origin", "-d", "gitcore-0.0.1")

			logging.Info(context.Background(), "Stdout: %s, Stderr: %s", stdout, stderr)
			require.NotNil(t, err)
			require.Contains(t, stderr, "prevent_deletion")
		})

		t.Run("check prevent_all_changes", func(t *testing.T) {
			cg.Must(t, "checkout", "legacy/feature1")
			suite.makeNewFileWithContent(cg, "3.txt", "hello")
			cg.Must(t, "add", ".")
			cg.Must(t, "commit", "-m", "update to legacy branch")

			stdout, stderr, err := cg.Exec("push")

			logging.Info(context.Background(), "Stdout: %s, Stderr: %s", stdout, stderr)
			require.NotNil(t, err)
			require.Contains(t, stderr, "prevent_all_changes")
		})

		t.Run("check non_pr_changes", func(t *testing.T) {
			cg.Must(t, "checkout", "nonpr")
			suite.makeNewFileWithContent(cg, "11.txt", "hello")
			cg.Must(t, "add", ".")
			cg.Must(t, "commit", "-m", "direct update to nonpr")

			stdout, stderr, err := cg.Exec("push")

			logging.Info(context.Background(), "Stdout: %s, Stderr: %s", stdout, stderr)
			require.NotNil(t, err)
			require.Contains(t, stderr, "non_pr_changes")

			cg.Must(t, "reset", "--hard", "origin/nonpr")
			cg.Must(t, "clean", "-d", "--force")

		})

		t.Run("check non_pr_changes - happy path", func(t *testing.T) {
			cg.Must(t, "checkout", "-b", "feature", "nonpr")
			suite.makeNewFileWithContent(cg, "11.txt", "hello")
			suite.commitAll(cg, "my pr", "feature")

			repo, err := suite.RepoRepo.GetRepositoryByID(context.Background(), repoID)
			require.NoError(t, err)

			client := pb.NewPRServiceClient(suite.grpcClient)
			pr := suite.makePullRequest(suite.users.Kopatych, &makePrOptions{
				Repo:    repo,
				Source:  "feature",
				Target:  "nonpr",
				Publish: yautils.PtrFromValue(true),
			})

			//suite.addRole(t, suite.users.Kopatych, suite.repos.Alpha, iam.Roles.RepositoriesContributor)
			ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)

			_, err = client.Merge(ctx, &pb.MergeRequest{
				PrId:  grpc.MarshalID(pr.ID),
				Force: true,
			})
			require.NoError(t, err)
			suite.WaitForWorkflows(t, entities.WorkflowTypes.MergeV2PullRequest)

			// pr must be merged without errors
			pr, err = suite.PullRequestRepo.Get(context.Background(), pr.ID)
			require.NoError(t, err)

			require.Equal(t, entities.PRStatuses.Merged, pr.Status)
		})

		t.Run("branch creation is allowed", func(t *testing.T) {
			cg.Must(t, "checkout", "-b", "protected/feature1", "master")
			suite.makeNewFileWithContent(cg, "feature1.txt", "hello")
			suite.commitAll(cg, "add feature1", "protected/feature1")

			// Try to push more commits to the branch - should fail
			suite.makeNewFileWithContent(cg, "feature2.txt", "world")
			cg.Must(t, "add", ".")
			cg.Must(t, "commit", "-m", "update feature1")

			_, stderr, err := cg.Exec("push")
			require.NotNil(t, err, "Branch update should be blocked")
			require.Contains(t, stderr, "prevent_non_pr_changes")
		})

		suite.AfterTest("", "")
	}
}

// Tests that any new yaml path prevents old config usage
func (suite *RwApiTestSuite) TestBranchPolicyConfigClash() {
	t := suite.T()

	protocol := suite.HTTPSProtocol()
	_, orgSlug, repoSlug := suite.makeRandomRepo(t, suite.users.Kopatych)

	tmpDir := testutils.TempDir(t, "", "testrepo")
	w := testutils.NewWorkdir(t, tmpDir)

	cg := protocol.PrepareCGit(w, testutils.UserIdentities.Kopatych)

	cg.Must(t, "init", ".")
	cg.Must(t, "remote", "add", "origin", protocol.RepoURL(orgSlug, repoSlug))

	cg.Must(t, "branch", "-m", "master")
	suite.makeNewFileWithContent(cg, "1.txt", "hello")
	suite.commitAll(cg, "initial", "master")

	cg.Must(t, "checkout", "-b", "legacy/feature1")
	suite.makeNewFileWithContent(cg, "2.txt", "hello")
	suite.commitAll(cg, "feature1", "legacy/feature1")

	cg.Must(t, "checkout", "-b", "nonpr", "master")
	suite.makeNewFileWithContent(cg, "onlypr", "hello")
	suite.commitAll(cg, "onlypr", "nonpr")

	rules := `
branch_protection:
  policies:
    - target: branch
      matches: master
      message: "do not use force"
      rules:
        - prevent_force_push
    - target: branch
      matches: nonpr
      message: "non pr change"
      rules:
        - prevent_non_pr_changes
    - target: branch
      matches: releases/**
      message: "do not create"
      rules:
        - prevent_creation
    - target: tag
      matches: gitcore-* 
      message: "do not delete"
      rules:
        - prevent_deletion
    - target: branch
      matches: legacy/** 
      message: "any change to such branch is forbidden"
      rules:
        - prevent_all_changes
`

	cg.Must(t, "checkout", "master")
	suite.makeNewFileWithContent(cg, oyaml.ReviewPath, "")
	suite.makeNewFileWithContent(cg, oyaml.OldPath, rules)
	suite.commitAll(cg, "add config", "master")

	t.Run("ignore old config", func(t *testing.T) {
		cg.Must(t, "checkout", "legacy/feature1")
		suite.makeNewFileWithContent(cg, "3.txt", "hello")
		cg.Must(t, "add", ".")
		cg.Must(t, "commit", "-m", "update to legacy branch")

		_, _, err := cg.Exec("push")

		require.Nil(t, err)
	})

	t.Run("use new config", func(t *testing.T) {
		cg.Must(t, "checkout", "master")
		suite.makeNewFileWithContent(cg, oyaml.BranchPolicyPath, rules)
		suite.commitAll(cg, "add config", "master")

		cg.Must(t, "checkout", "legacy/feature1")
		suite.makeNewFileWithContent(cg, "4.txt", "hello")
		cg.Must(t, "add", ".")
		cg.Must(t, "commit", "-m", "update to legacy branch")

		_, stderr, err := cg.Exec("push")

		require.NotNil(t, err)
		require.Contains(t, stderr, "prevent_all_changes")
	})
}
