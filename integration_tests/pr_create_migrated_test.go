package integrationtests

import (
	"common/logging"
	"common/utils/rolesgenerator/iam"
	"context"
	"fmt"
	"gitcore/internal/entities"
	"gitcore/internal/interfaces"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/stretchr/testify/require"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"testing"
)

func (suite *RwApiTestSuite) TestCreateMigratedPR_Default() {
	t := suite.T()
	ctx := context.Background()

	const unmergedPRID = 1
	const (
		unmergedBranchName = "unmerged-source-branch"
		defaultBranchName  = "master"
	)

	repo := suite.repos.GithubMigrated
	logging.Info(ctx, "Repo: %d", repo.ID)
	suite.addRole(t, suite.users.Kopatych, repo, iam.Roles.RepositoriesAdmin)
	authenticator := suite.getFakeAuthenticator(suite.users.Kopatych.Identity)

	refRepo := suite.RefRepoFactory.Build(repo.ID)
	unmergedBranch, err := refRepo.GetReferenceByName(ctx, plumbing.NewBranchReferenceName(unmergedBranchName))
	require.NoError(t, err)
	defaultBranch, err := refRepo.GetReferenceByName(ctx, plumbing.NewBranchReferenceName(defaultBranchName))
	require.NoError(t, err)

	// Create a PR with MigratedPullRequest data
	pr := &entities.PullRequest{
		Title:        "Test Migrated PR",
		Description:  "Testing migrated PR with MergeBaseRef and MergeHeadRef",
		RepoID:       repo.ID,
		SourceRepoID: repo.ID,
		AuthorID:     suite.users.Kopatych.ID,
		Status:       entities.PRStatuses.Open,
		SourceBranch: unmergedBranchName,
		TargetBranch: defaultBranchName,
		HeadHash:     unmergedBranch.Hash(),
		Conflicts: entities.PRConflict{
			Hash:   defaultBranch.Hash(),
			Status: entities.OperationStatuses.Failed,
			Items: []entities.MergeConflict{
				{
					Type:    0,
					Path:    "<unknown>",
					Message: "Message",
				},
			},
		},
		MigratedPullRequest: &entities.MigratedPullRequest{
			RepoID:       repo.ID,
			Provider:     int32(pb.GetProviderFeaturesResponse_PROVIDER_GITHUB),
			OriginalID:   unmergedPRID,
			MergeBaseRef: "",
			MergeHeadRef: unmergedBranchName,
			URL:          fmt.Sprintf("https://github.com/example/repo/pull/%d", unmergedPRID),
		},
	}

	// Create the migrated PR
	prID, err := suite.PullRequestService.CreateMigratedPR(
		ctx,
		pr,
		defaultBranch.Hash(),
		nil,        // Resolve merge base the usual way
		[]uint64{}, // No reviewers
		suite.orgs.Yandex.ID,
		suite.users.Kopatych,
		authenticator,
		entities.NotifyOptions{},
	)
	require.NoError(t, err)

	// Verify the PR was created correctly
	createdPR, err := suite.PullRequestService.Get(ctx, prID)
	require.NoError(t, err)
	require.Equal(t, pr.Title, createdPR.Title)
	require.Equal(t, pr.Description, createdPR.Description)
	require.Equal(t, pr.RepoID, createdPR.RepoID)
	require.Equal(t, pr.AuthorID, createdPR.AuthorID)
	require.Equal(t, pr.Status, createdPR.Status)
	require.Equal(t, pr.SourceBranch, createdPR.SourceBranch)
	require.Equal(t, pr.TargetBranch, createdPR.TargetBranch)
	require.Equal(t, pr.HeadHash, createdPR.HeadHash)
	require.Equal(t, pr.Conflicts, createdPR.Conflicts)

	// Verify the migrated PR data
	migratedPR, err := suite.MigratedPullRequestRepo.Get(ctx, prID)
	require.NoError(t, err)
	require.NotNil(t, migratedPR)
	require.Equal(t, pr.MigratedPullRequest.RepoID, migratedPR.RepoID)
	require.Equal(t, pr.MigratedPullRequest.Provider, migratedPR.Provider)
	require.Equal(t, pr.MigratedPullRequest.OriginalID, migratedPR.OriginalID)
	require.Equal(t, pr.MigratedPullRequest.MergeBaseRef, migratedPR.MergeBaseRef)
	require.Equal(t, pr.MigratedPullRequest.MergeHeadRef, migratedPR.MergeHeadRef)
	require.Equal(t, pr.MigratedPullRequest.URL, migratedPR.URL)

	// Get merge base
	bases, err := suite.GitcoreClient.GetMergeBases(ctx, authenticator, interfaces.MergeBasesArgs{
		Target: entities.NewGitRevisionFromCommit(repo.ID, defaultBranch.Hash()),
		Source: entities.NewGitRevisionFromCommit(repo.ID, unmergedBranch.Hash()),
	})
	require.NoError(t, err)
	require.NotEmpty(t, bases, "No merge bases found")
	mergeBaseHash := bases[0]

	// Get the PR iteration to verify the merge base hash
	prRepo := suite.PullRequestRepoFactory.Build(repo.ID)
	iter, err := prRepo.GetIteration(ctx, prID, createdPR.Iteration)
	require.NoError(t, err)
	require.Equal(t, mergeBaseHash.Commit, iter.MergebaseHash)
}

func (suite *RwApiTestSuite) TestCreateMigratedPR_SpecialCases() {
	t := suite.T()
	ctx := context.Background()

	const (
		mergedBranchName           = "merged-source-branch"
		mergedBranchGithubRefName  = "refs/pulls/2/head"
		deletedBranchName          = "deleted-source-branch"
		deletedBranchGithubRefName = "refs/pulls/3/head"
		defaultBranchName          = "master"
	)

	repo := suite.repos.GithubMigrated
	logging.Info(ctx, "Repo: %d", repo.ID)
	suite.addRole(t, suite.users.Kopatych, repo, iam.Roles.RepositoriesAdmin)
	authenticator := suite.getFakeAuthenticator(suite.users.Kopatych.Identity)

	// Get references for the branches
	refRepo := suite.RefRepoFactory.Build(repo.ID)
	defaultBranch, err := refRepo.GetReferenceByName(ctx, plumbing.NewBranchReferenceName(defaultBranchName))
	require.NoError(t, err)

	var publicCounter uint64
	makePR := func(sourceBranch, targetBranch string, status entities.PRStatus, headHash plumbing.Hash) *entities.PullRequest {
		publicCounter++
		return &entities.PullRequest{
			PublicID:     publicCounter,
			Title:        "Title",
			Description:  "Description",
			RepoID:       repo.ID,
			AuthorID:     suite.users.Kopatych.ID,
			HeadHash:     headHash,
			Status:       status,
			SourceRepoID: repo.ID,
			SourceBranch: sourceBranch,
			TargetBranch: targetBranch,
			MigratedPullRequest: &entities.MigratedPullRequest{
				RepoID:     repo.ID,
				PublicID:   publicCounter,
				Provider:   int32(pb.GetProviderFeaturesResponse_PROVIDER_GITHUB),
				OriginalID: int64(publicCounter),
			},
		}
	}

	requireMergeBase := func(t *testing.T, prID uint64, headHash plumbing.Hash) {
		createdPR, err := suite.PullRequestService.Get(ctx, prID)
		require.NoError(t, err)

		mergeBases, err := suite.GitcoreClient.GetMergeBases(ctx, authenticator, interfaces.MergeBasesArgs{
			Target: entities.NewGitRevisionFromCommit(repo.ID, defaultBranch.Hash()),
			Source: entities.NewGitRevisionFromCommit(repo.ID, headHash),
		})
		require.NoError(t, err)
		require.Len(t, mergeBases, 1)

		prRepo := suite.PullRequestRepoFactory.Build(repo.ID)
		iter, err := prRepo.GetIteration(ctx, prID, createdPR.Iteration)
		require.NoError(t, err)
		require.Equal(t, mergeBases[0].Commit.String(), iter.MergebaseHash.String())
	}

	t.Run("already merged", func(t *testing.T) {
		head, err := refRepo.GetReferenceByName(ctx, mergedBranchGithubRefName)
		require.NoError(t, err)

		pr := makePR(mergedBranchName, defaultBranchName, entities.PRStatuses.Merged, head.Hash())
		prID, err := suite.PullRequestService.CreateMigratedPR(
			ctx,
			pr, defaultBranch.Hash(), nil, []uint64{},
			suite.orgs.Yandex.ID, suite.users.Kopatych, authenticator,
			entities.NotifyOptions{},
		)
		require.NoError(t, err)
		requireMergeBase(t, prID, head.Hash())
	})

	t.Run("deleted source branch", func(t *testing.T) {
		head, err := refRepo.GetReferenceByName(ctx, deletedBranchGithubRefName)
		require.NoError(t, err)

		pr := makePR(deletedBranchName, defaultBranchName, entities.PRStatuses.Open, head.Hash())
		prID, err := suite.PullRequestService.CreateMigratedPR(
			ctx,
			pr, defaultBranch.Hash(), nil, []uint64{},
			suite.orgs.Yandex.ID, suite.users.Kopatych, authenticator,
			entities.NotifyOptions{},
		)
		require.NoError(t, err)
		requireMergeBase(t, prID, head.Hash())
	})

	t.Run("no merge bases", func(t *testing.T) {
		suite.mustBash(repo, `
			git checkout master
			git checkout --orphan new-root
			echo "This is a new root" > README.md
			git commit -m "New root"
		`)

		head, err := refRepo.GetReferenceByName(ctx, plumbing.NewBranchReferenceName("new-root"))
		require.NoError(t, err)

		pr := makePR("new-root", defaultBranchName, entities.PRStatuses.Open, head.Hash())
		prID, err := suite.PullRequestService.CreateMigratedPR(
			ctx,
			pr, defaultBranch.Hash(), nil, []uint64{},
			suite.orgs.Yandex.ID, suite.users.Kopatych, authenticator,
			entities.NotifyOptions{},
		)
		require.NoError(t, err)

		createdPR, err := suite.PullRequestService.Get(ctx, prID)
		require.NoError(t, err)
		prRepo := suite.PullRequestRepoFactory.Build(repo.ID)
		iter, err := prRepo.GetIteration(ctx, prID, createdPR.Iteration)
		require.NoError(t, err)
		require.Equal(t, plumbing.ZeroHash.String(), iter.MergebaseHash.String())
	})
}
