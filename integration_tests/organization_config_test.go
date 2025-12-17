package integrationtests

import (
	"common/oyaml"
	"common/utils/rolesgenerator/iam"
	"context"
	"gitcore/internal/entities"
	"gitcore/internal/interfaces"
	"gitcore/internal/testutils"
	"os"
	"path"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func (suite *RwApiTestSuite) TestOrganizationConfig() {
	type testCase struct {
		name           string
		repoSlug       string
		files          map[string]string
		expectConfig   bool
		validateConfig func(t *testing.T, config *oyaml.ConfigAsCode)
	}

	tests := []testCase{
		{
			name:     "happy path with all configs",
			repoSlug: oyaml.SourceCraftSettingRepository,
			files: map[string]string{
				oyaml.ReviewPath: `codereview:
                  need_ships: 2
                  ignore_self_ship: true
                `,
				oyaml.BranchPolicyPath: `branch_protection:
                  policies:
                    - target: default_branch
                      matches: ["main"]
                      rules: [prevent_force_push]
                `,
				oyaml.WebhooksPath: `webhooks:
                  hooks:
                    - slug: test-webhook
                      url: https://example.com/webhook
                  on:
                    push:
                      - hooks: ["test-webhook"]
                `,
			},
			expectConfig: true,
			validateConfig: func(t *testing.T, config *oyaml.ConfigAsCode) {
				require.NotNil(t, config.CodeReview.NeedShips)
				require.Equal(t, 2, *config.CodeReview.NeedShips)
				require.NotNil(t, config.CodeReview.IgnoreSelfShip)
				require.True(t, *config.CodeReview.IgnoreSelfShip)

				require.Len(t, config.BranchProtection.Policies, 1)
				require.Equal(t, "default_branch", string(config.BranchProtection.Policies[0].Target))

				require.Len(t, config.Webhooks.Hooks, 1)
				require.Equal(t, "test-webhook", config.Webhooks.Hooks[0].Slug)
				require.Equal(t, "https://example.com/webhook", config.Webhooks.Hooks[0].URL)
			},
		},
		{
			name:     "not sourcecraft repo",
			repoSlug: "normal-repo",
			files: map[string]string{
				oyaml.ReviewPath: `codereview:
				  need_ships: 3
				`,
			},
			expectConfig: false,
			validateConfig: func(t *testing.T, config *oyaml.ConfigAsCode) {
				require.Nil(t, config.CodeReview.NeedShips, "config should not be created for non-.sourcecraft repo")
			},
		},
		{
			name:     "no sourcecraft directory",
			repoSlug: oyaml.SourceCraftSettingRepository,
			files: map[string]string{
				"README.md": "# Test repo\n",
			},
			expectConfig: false,
			validateConfig: func(t *testing.T, config *oyaml.ConfigAsCode) {
				require.Nil(t, config.CodeReview.NeedShips, "config should be empty when no .sourcecraft directory")
			},
		},
	}

	for _, tc := range tests {
		suite.Run(tc.name, func() {
			t := suite.T()
			ctx := context.Background()

			repoID, err := suite.RepoRepo.CreateRepository(ctx, &interfaces.CreateRepositoryArgs{
				Slug:       tc.repoSlug,
				OrgID:      suite.orgs.Yandex.ID,
				CreatedBy:  suite.users.Kopatych.ID,
				Visibility: entities.Visibilities.Public,
			})
			require.NoError(t, err)

			repo, err := suite.RepoRepo.GetRepositoryByID(ctx, repoID)
			require.NoError(t, err)

			suite.addRole(t, suite.users.Kopatych, repo, iam.Roles.RepositoriesAdmin)

			// Setup git repository
			protocol := suite.HTTPSProtocol()
			tmpDir := testutils.TempDir(t, "", "test-org-config")
			w := testutils.NewWorkdir(t, tmpDir)
			cg := protocol.PrepareCGit(w, testutils.UserIdentities.Kopatych)

			// Initialize git repository
			cg.Must(t, "init", ".")
			cg.Must(t, "branch", "-m", "master")
			cg.Must(t, "remote", "add", "origin", protocol.RepoURL(repo.OrgSlug, repo.Slug))

			// Create files
			for filePath, content := range tc.files {
				fileDir := path.Dir(path.Join(cg.Path(), filePath))
				err := os.MkdirAll(fileDir, 0755)
				require.NoError(t, err)

				f, err := os.Create(path.Join(cg.Path(), filePath))
				require.NoError(t, err)

				_, err = f.WriteString(content)
				require.NoError(t, err)

				err = f.Close()
				require.NoError(t, err)
			}

			// Commit and push
			cg.Must(t, "add", ".")
			cg.Must(t, "commit", "-m", "Add configuration files")
			cg.Must(t, "push", "-u", "origin", "master")

			// Get organization config
			config, err := suite.OrgConfigService.GetConfig(ctx, repo.OrgID)
			require.NoError(t, err)
			require.NotNil(t, config)
			tc.validateConfig(t, config)
		})
	}
}

func (suite *RwApiTestSuite) TestOrganizationConfigUpdate() {
	t := suite.T()
	ctx := context.Background()

	// Create .sourcecraft repo
	repoID, err := suite.RepoRepo.CreateRepository(ctx, &interfaces.CreateRepositoryArgs{
		Slug:       oyaml.SourceCraftSettingRepository,
		OrgID:      suite.orgs.Yandex.ID,
		CreatedBy:  suite.users.Kopatych.ID,
		Visibility: entities.Visibilities.Public,
	})
	require.NoError(t, err)

	repo, err := suite.RepoRepo.GetRepositoryByID(ctx, repoID)
	require.NoError(t, err)

	suite.addRole(t, suite.users.Kopatych, repo, iam.Roles.RepositoriesAdmin)

	// Setup git repository
	protocol := suite.HTTPSProtocol()
	tmpDir := testutils.TempDir(t, "", "test-org-config-update")
	w := testutils.NewWorkdir(t, tmpDir)
	cg := protocol.PrepareCGit(w, testutils.UserIdentities.Kopatych)

	// Initialize git repository
	cg.Must(t, "init", ".")
	cg.Must(t, "branch", "-m", "master")
	cg.Must(t, "remote", "add", "origin", protocol.RepoURL(repo.OrgSlug, repo.Slug))

	// First push: need_ships = 2
	err = os.MkdirAll(path.Join(cg.Path(), oyaml.SourceCraftDirectory), 0755)
	require.NoError(t, err)

	f, err := os.Create(path.Join(cg.Path(), oyaml.ReviewPath))
	require.NoError(t, err)
	_, err = f.WriteString(`codereview:
  need_ships: 2
`)
	require.NoError(t, err)
	err = f.Close()
	require.NoError(t, err)

	cg.Must(t, "add", ".")
	cg.Must(t, "commit", "-m", "Initial config")
	cg.Must(t, "push", "-u", "origin", "master")

	// Verify initial config
	config, err := suite.OrgConfigService.GetConfig(ctx, repo.OrgID)
	require.NoError(t, err)
	require.NotNil(t, config.CodeReview.NeedShips)
	require.Equal(t, 2, *config.CodeReview.NeedShips)

	// Second push: need_ships = 5
	f, err = os.Create(path.Join(cg.Path(), oyaml.ReviewPath))
	require.NoError(t, err)
	_, err = f.WriteString(`codereview:
  need_ships: 5
`)
	require.NoError(t, err)
	err = f.Close()
	require.NoError(t, err)

	cg.Must(t, "add", ".")
	cg.Must(t, "commit", "-m", "Update config")
	cg.Must(t, "push", "origin", "master")

	// Verify updated config
	config, err = suite.OrgConfigService.GetConfig(ctx, repo.OrgID)
	require.NoError(t, err)
	require.NotNil(t, config.CodeReview.NeedShips)
	require.Equal(t, 5, *config.CodeReview.NeedShips, "config should be updated after second push")
}

func (suite *RwApiTestSuite) TestOrganizationConfigOnlyDefaultBranch() {
	t := suite.T()
	ctx := context.Background()

	// Create .sourcecraft repo
	repoID, err := suite.RepoRepo.CreateRepository(ctx, &interfaces.CreateRepositoryArgs{
		Slug:       oyaml.SourceCraftSettingRepository,
		OrgID:      suite.orgs.Yandex.ID,
		CreatedBy:  suite.users.Kopatych.ID,
		Visibility: entities.Visibilities.Public,
	})
	require.NoError(t, err)

	repo, err := suite.RepoRepo.GetRepositoryByID(ctx, repoID)
	require.NoError(t, err)

	suite.addRole(t, suite.users.Kopatych, repo, iam.Roles.RepositoriesAdmin)

	// Setup git repository
	protocol := suite.HTTPSProtocol()
	tmpDir := testutils.TempDir(t, "", "test-org-config-branch")
	w := testutils.NewWorkdir(t, tmpDir)
	cg := protocol.PrepareCGit(w, testutils.UserIdentities.Kopatych)

	// Initialize git repository with master branch first
	cg.Must(t, "init", ".")
	cg.Must(t, "branch", "-m", "master")
	cg.Must(t, "remote", "add", "origin", protocol.RepoURL(repo.OrgSlug, repo.Slug))

	// Create initial commit on master
	f, err := os.Create(path.Join(cg.Path(), "README.md"))
	require.NoError(t, err)
	_, err = f.WriteString("# Test repo\n")
	require.NoError(t, err)
	err = f.Close()
	require.NoError(t, err)

	cg.Must(t, "add", ".")
	cg.Must(t, "commit", "-m", "Initial commit")
	cg.Must(t, "push", "-u", "origin", "master")

	// Create feature branch and push config there
	cg.Must(t, "checkout", "-b", "feature-branch")

	err = os.MkdirAll(path.Join(cg.Path(), oyaml.SourceCraftDirectory), 0755)
	require.NoError(t, err)

	f, err = os.Create(path.Join(cg.Path(), oyaml.ReviewPath))
	require.NoError(t, err)
	_, err = f.WriteString(`codereview:
  need_ships: 3
`)
	require.NoError(t, err)
	err = f.Close()
	require.NoError(t, err)

	cg.Must(t, "add", ".")
	cg.Must(t, "commit", "-m", "Add config in feature branch")
	cg.Must(t, "push", "-u", "origin", "feature-branch")

	// Config should NOT be created for feature branch
	config, err := suite.OrgConfigService.GetConfig(ctx, repo.OrgID)
	require.NoError(t, err)
	require.Nil(t, config.CodeReview.NeedShips, "config should not be created for non-default branch")

	// Now merge to master
	cg.Must(t, "checkout", "master")
	cg.Must(t, "merge", "feature-branch")
	cg.Must(t, "push", "origin", "master")

	// Now config should be created
	config, err = suite.OrgConfigService.GetConfig(ctx, repo.OrgID)
	require.NoError(t, err)
	require.NotNil(t, config.CodeReview.NeedShips)
	require.Equal(t, 3, *config.CodeReview.NeedShips, "config should be created after merge to default branch")
}

func (suite *RwApiTestSuite) TestOrganizationConfigBranchProtection() {
	t := suite.T()
	ctx := context.Background()

	// create some repo
	normalRepoID, orgSlug, normalRepoSlug := suite.makeRandomRepo(t, suite.users.Kopatych)
	normalRepo, err := suite.RepoRepo.GetRepositoryByID(ctx, normalRepoID)
	require.NoError(t, err)

	suite.addRole(t, suite.users.Kopatych, normalRepo, iam.Roles.RepositoriesAdmin)

	// Initialize normal repository
	protocol := suite.HTTPSProtocol()
	tmpDirNormal := testutils.TempDir(t, "", "test-normal-repo")
	wNormal := testutils.NewWorkdir(t, tmpDirNormal)
	cgNormal := protocol.PrepareCGit(wNormal, testutils.UserIdentities.Kopatych)

	cgNormal.Must(t, "init", ".")
	cgNormal.Must(t, "branch", "-m", "master")
	cgNormal.Must(t, "remote", "add", "origin", protocol.RepoURL(orgSlug, normalRepoSlug))
	suite.makeNewFileWithContent(cgNormal, "README.md", "# Test repo\n")
	suite.commitAll(cgNormal, "Initial commit", "master")

	// Create .sourcecraft repo with branch protection settings
	sourcecraftRepoID, err := suite.RepoRepo.CreateRepository(ctx, &interfaces.CreateRepositoryArgs{
		Slug:       oyaml.SourceCraftSettingRepository,
		OrgID:      normalRepo.OrgID,
		CreatedBy:  suite.users.Kopatych.ID,
		Visibility: entities.Visibilities.Public,
	})
	require.NoError(t, err)

	sourcecraftRepo, err := suite.RepoRepo.GetRepositoryByID(ctx, sourcecraftRepoID)
	require.NoError(t, err)
	suite.addRole(t, suite.users.Kopatych, sourcecraftRepo, iam.Roles.RepositoriesAdmin)

	require.Equal(t, normalRepo.OrgID, sourcecraftRepo.OrgID, "normal repo must be in the same org as .sourcecraft")

	// Create branch protection config with prevent_creation rule
	suite.addFile(sourcecraftRepo, "master", oyaml.BranchPolicyPath, []byte(`
branch_protection:
  policies:
    - target: branch
      matches: releases/**
      message: "Cannot create release branches due to organization policy"
      rules:
        - prevent_creation
`))

	t.Run("Verify org config", func(t *testing.T) {
		orgConfig, err := suite.OrgConfigService.GetConfigForRepo(ctx, normalRepo.ID)
		require.NoError(t, err)
		require.Len(t, orgConfig.BranchProtection.Policies, 1)
		require.Equal(t, "releases/**", orgConfig.BranchProtection.Policies[0].Matches[0])
	})

	t.Run("prevent creation of releases branch", func(t *testing.T) {
		cgNormal.Must(t, "checkout", "-b", "releases/v1.0.0")
		suite.makeNewFileWithContent(cgNormal, "release.txt", "Release version 1.0.0\n")
		cgNormal.Must(t, "add", ".")
		cgNormal.Must(t, "commit", "-m", "Prepare release")

		// This push should fail due to organization branch protection policy
		_, stderr, err := cgNormal.Exec("push", "-u", "origin", "releases/v1.0.0")
		require.NotNil(t, err, "push should fail due to prevent_creation rule")
		require.Contains(t, stderr, "prevent_creation", "error message should mention prevent_creation rule")
	})

	t.Run("allow creation of normal branches", func(t *testing.T) {
		cgNormal.Must(t, "checkout", "master")
		cgNormal.Must(t, "checkout", "-b", "feature/test-feature")
		suite.makeNewFileWithContent(cgNormal, "feature.txt", "New feature\n")
		suite.commitAll(cgNormal, "Add feature", "feature/test-feature")

		// This push should succeed
		cgNormal.Must(t, "push", "-u", "origin", "feature/test-feature")
	})
}

func (suite *RwApiTestSuite) TestOrganizationConfigValidation() {
	t := suite.T()
	ctx := context.Background()

	sourcecraftRepoID, err := suite.RepoRepo.CreateRepository(ctx, &interfaces.CreateRepositoryArgs{
		Slug:       oyaml.SourceCraftSettingRepository,
		OrgID:      suite.orgs.Yandex.ID,
		CreatedBy:  suite.users.Kopatych.ID,
		Visibility: entities.Visibilities.Public,
	})
	require.NoError(t, err)

	sourcecraftRepo, err := suite.RepoRepo.GetRepositoryByID(ctx, sourcecraftRepoID)
	require.NoError(t, err)
	suite.addRole(t, suite.users.Kopatych, sourcecraftRepo, iam.Roles.RepositoriesAdmin)

	// Initialize git workdir
	protocol := suite.HTTPSProtocol()
	tmpDir := testutils.TempDir(t, "", "test-org-config-validation")
	w := testutils.NewWorkdir(t, tmpDir)
	cg := protocol.PrepareCGit(w, testutils.UserIdentities.Kopatych)

	cg.Must(t, "init", ".")
	cg.Must(t, "branch", "-m", "master")
	cg.Must(t, "remote", "add", "origin", protocol.RepoURL(sourcecraftRepo.OrgSlug, sourcecraftRepo.Slug))

	validConfig := []byte(`
codereview:
  need_ships: 3
  ignore_self_ship: true
`)

	invalidBranchPolicyConfig := []byte(`branch_protection: bad syntax here:::`)
	invalidCodeReviewConfig := []byte(`
codereview:
  need_ships: asn
  ignore_self_ship: 
	- 1
    - 2
  ignore_non_reviewers_block: false
  auto_assign: true
  rules:
    - some:
        - 11`)

	invalidWebhooksConfig := []byte(`
webhooks:
  hooks:
    - slug: wh1
      name: ""
      description: "Triggers on main branch pushes"
      secret: "my-secret"
      ssl_verification: true
      active: true
on:
  push:
    - hooks: ["wh2"]
`)

	t.Run("Push invalid config to empty repo (first commit)", func(t *testing.T) {
		defer func() {
			require.NoError(t, os.RemoveAll(path.Join(cg.Path(), oyaml.SourceCraftDirectory)))
		}()

		require.NoError(t, os.MkdirAll(path.Join(cg.Path(), oyaml.SourceCraftDirectory), 0755))
		require.NoError(t, os.WriteFile(path.Join(cg.Path(), oyaml.BranchPolicyPath), invalidBranchPolicyConfig, 0644))
		require.NoError(t, os.WriteFile(path.Join(cg.Path(), oyaml.ReviewPath), invalidCodeReviewConfig, 0644))
		require.NoError(t, os.WriteFile(path.Join(cg.Path(), oyaml.WebhooksPath), invalidWebhooksConfig, 0644))

		cg.Must(t, "add", ".")
		cg.Must(t, "commit", "-m", "Invalid config")

		_, stderr, err := cg.Exec("push", "origin", "master")
		require.NotNil(t, err)
		require.Equal(t, strings.TrimSpace(`
remote: error: Config validation failed:        
remote: error:         
remote: error:   .sourcecraft/branches.yaml        
remote: error:     yaml: mapping values are not allowed in this context        
remote: error:         
remote: error:   .sourcecraft/review.yaml        
remote: error:     yaml: line 4: found character that cannot start any token        
remote: error:         
remote: error:   .sourcecraft/webhooks.yaml        
remote: error:     webhook URL is required at 3:7        
remote: error:         
remote: error: Please fix the errors and try again.        
remote: error: Documentation: https://sourcecraft.dev/portal/docs/sourcecraft/concepts/branch-policies        
To http://localhost:8081/yandex/.sourcecraft.git
 ! [remote rejected] master -> master (syntax error in config file .sourcecraft/webhooks.yaml: yaml: unmarshal errors:
  webhook URL is required at 3:7)
error: failed to push some refs to 'http://localhost:8081/yandex/.sourcecraft.git'
`), strings.TrimSpace(stderr))
	})

	t.Run("Push valid config", func(t *testing.T) {
		require.NoError(t, os.MkdirAll(path.Join(cg.Path(), oyaml.SourceCraftDirectory), 0755))
		require.NoError(t, os.WriteFile(path.Join(cg.Path(), oyaml.ReviewPath), validConfig, 0644))
		suite.commitAll(cg, "initial commit", "master")
	})

	t.Run("Push invalid config to default branch should be rejected", func(t *testing.T) {
		defer func() {
			require.NoError(t, os.RemoveAll(path.Join(cg.Path(), oyaml.SourceCraftDirectory)))
		}()

		invalidConfig := []byte(`
branch_protection:
  policies:
    - target: branch
      matches: ["OO/[abc12345"]
      message: "do not merge in feature branches, use rebase"
      rules:
        - unknown_rule
`)

		require.NoError(t, os.MkdirAll(path.Join(cg.Path(), oyaml.SourceCraftDirectory), 0755))
		require.NoError(t, os.WriteFile(path.Join(cg.Path(), oyaml.BranchPolicyPath), invalidConfig, 0644))

		cg.Must(t, "add", ".")
		cg.Must(t, "commit", "-m", "Invalid config")

		_, stderr, err := cg.Exec("push", "origin", "master")
		require.NotNil(t, err)
		require.Equal(t, strings.TrimSpace(`
remote: error: Config validation failed:        
remote: error:         
remote: error:   .sourcecraft/branches.yaml        
remote: error:     invalid glob pattern OO/[abc12345 at 4:16        
remote: error:     unknown branch protection rule unknown_rule at 7:11        
remote: error:         
remote: error: Please fix the errors and try again.        
remote: error: Documentation: https://sourcecraft.dev/portal/docs/sourcecraft/concepts/branch-policies        
To http://localhost:8081/yandex/.sourcecraft.git
 ! [remote rejected] master -> master (syntax error in config file .sourcecraft/branches.yaml: yaml: unmarshal errors:
  invalid glob pattern OO/[abc12345 at 4:16
  unknown branch protection rule unknown_rule at 7:11)
error: failed to push some refs to 'http://localhost:8081/yandex/.sourcecraft.git'
`), strings.TrimSpace(stderr))
	})

	t.Run("Fix config and push valid config should succeed", func(t *testing.T) {
		require.NoError(t, os.MkdirAll(path.Join(cg.Path(), oyaml.SourceCraftDirectory), 0755))
		require.NoError(t, os.WriteFile(path.Join(cg.Path(), oyaml.ReviewPath), validConfig, 0644))
		suite.commitAll(cg, "Fix config", "master")
	})

	t.Run("Push invalid config to feature branch should succeed", func(t *testing.T) {
		cg.Must(t, "checkout", "-b", "feature/test-invalid")

		require.NoError(t, os.MkdirAll(path.Join(cg.Path(), oyaml.SourceCraftDirectory), 0755))
		require.NoError(t, os.WriteFile(path.Join(cg.Path(), oyaml.ReviewPath), invalidBranchPolicyConfig, 0644))
		cg.Must(t, "add", ".")
		cg.Must(t, "commit", "-m", "Invalid config in feature branch")
	})

	t.Run("Verify org config was updated with valid config only", func(t *testing.T) {
		orgConfig, err := suite.OrgConfigService.GetConfig(ctx, suite.orgs.Yandex.ID)
		require.NoError(t, err)
		require.NotNil(t, orgConfig.CodeReview)
		require.Equal(t, 3, *orgConfig.CodeReview.NeedShips)
		require.Equal(t, true, *orgConfig.CodeReview.IgnoreSelfShip)
	})
}
