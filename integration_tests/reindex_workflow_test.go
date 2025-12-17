package integrationtests

import (
	"common/cgit"
	"common/functools"
	"context"
	"encoding/json"
	"gitcore/internal/adapters/opensearch/mappings"
	"gitcore/internal/entities"
	"gitcore/internal/housekeeping"
	"gitcore/internal/interfaces"
	"gitcore/internal/revision"
	"gitcore/internal/services/index"
	"gitcore/pkg/pagination"
	"math/rand"
	"strconv"

	"github.com/stretchr/testify/require"
)

type TypeWrapper struct {
	MappingType string `json:"mapping_type"`
}

func (suite *RwApiTestSuite) TestReindexWorkflow() {
	t := suite.T()
	ctx := context.Background()

	// cleanup index
	suite.OpensearchKit.RecreateIndices(ctx)

	suite.cfg.OpenSearch.Enabled = false

	repo := suite.repos.TreeDiff
	suite.createIssuesForReindex(repo)
	suite.createPrsForReindex(repo)

	suite.cfg.OpenSearch.Enabled = true

	// execute workflow
	execution, err := suite.Scheduler.ExecuteWorkflow(
		ctx,
		index.ReindexWorkflowID("reindex"),
		entities.WorkflowTypes.SearchReindexWorkflow,
		index.ReindexWorkflowParams{
			Entities:       []revision.EntityType{revision.EntityTypes.User, revision.EntityTypes.Issue, revision.EntityTypes.PullRequest},
			IdempotencyKey: "reindex",
		})
	require.NoError(t, err)

	err = execution.Get(ctx, nil)
	require.NoError(t, err)

	err = suite.OpensearchKit.RefreshIndex(ctx)
	require.NoError(t, err)

	// check entities are in index
	hits, err := suite.OpensearchKit.GetDocuments(ctx)
	require.NoError(t, err)

	docs := functools.SliceToMapKV(hits, func(hit json.RawMessage) (uint64, mappings.IndexDocument) {
		typeDoc := TypeWrapper{}
		err := json.Unmarshal(hit, &typeDoc)
		require.NoError(t, err)
		switch typeDoc.MappingType {
		case "user":
			doc := mappings.UserDocument{}
			err := json.Unmarshal(hit, &doc)
			require.NoError(t, err)
			return doc.ID, &doc
		case "issue":
			doc := mappings.IssueDocument{}
			err := json.Unmarshal(hit, &doc)
			require.NoError(t, err)
			return doc.ID, &doc
		case "pull_request":
			doc := mappings.PullRequestDocument{}
			err := json.Unmarshal(hit, &doc)
			require.NoError(t, err)
			return doc.ID, &doc
		default:
			require.Failf(t, "Unknown mapping type %s", typeDoc.MappingType)
		}
		return 0, nil
	},
	)

	suite.UserRepo.GetAllUsers(ctx, func(user entities.User) error {
		doc, ok := docs[user.ID]
		if user.IsServiceAccount() || !user.IsOnboarded() || user.Identity.Src == entities.IdentityProviders.Migration {
			require.False(t, ok)
			return nil
		}
		require.True(t, ok)
		userDoc, ok := doc.(*mappings.UserDocument)
		require.True(t, ok)
		require.Equal(t, user.Username, userDoc.Username)
		require.Equal(t, user.FederationID, userDoc.FederationID)
		require.Equal(t, user.DisplayName, userDoc.PublicName)
		require.Equal(t, user.Email, userDoc.Email)

		repos, err := suite.UserRelevantReposRepository.GetRelevantRepos(ctx, user.ID)
		require.NoError(t, err)

		require.Equal(t, len(repos), len(userDoc.RelevantRepos))
		if len(repos) > 0 {
			require.EqualValues(t, repos, userDoc.RelevantRepos)
		}

		return nil
	})

	suite.IssueRepo.IterateByRepo(ctx,
		repo.ID, 10, func(batch []*entities.Issue) error {
			for _, issue := range batch {
				doc, ok := docs[issue.ID]
				require.True(t, ok)
				issueDoc, ok := doc.(*mappings.IssueDocument)
				require.True(t, ok)
				require.Equal(t, issue.PublicID, issueDoc.PublicID)
				require.Equal(t, strconv.FormatUint(issue.PublicID, 10), issueDoc.PublicIDKeyword)
				require.Equal(t, issue.Title, issueDoc.Title)
				require.Equal(t, issue.Description, issueDoc.Description)
				require.Equal(t, string(issue.Visibility), issueDoc.Visibility)
				require.Equal(t, issue.AssigneeID, issueDoc.AssigneeID)
				require.Equal(t, issue.AuthorID, issueDoc.AuthorID)
				require.Equal(t, issue.UpdatedBy, issueDoc.UpdatedBy)
			}
			return nil
		})

	res, err := suite.PullRequestRepo.List(ctx, interfaces.PullRequestListArgs{}, pagination.Options{})
	require.NoError(t, err)
	for _, pr := range res.Result {
		doc, ok := docs[pr.ID]
		require.True(t, ok)
		prDoc, ok := doc.(*mappings.PullRequestDocument)
		require.True(t, ok)
		require.Equal(t, pr.PublicID, prDoc.PublicID)
		require.Equal(t, strconv.FormatUint(pr.PublicID, 10), prDoc.PublicIDKeyword)
		require.Equal(t, pr.RepoID, prDoc.RepoID)
		require.Equal(t, pr.Title, prDoc.Title)
		require.Equal(t, pr.Description, prDoc.Description)
		require.Equal(t, pr.AuthorID, prDoc.AuthorID)
		require.Equal(t, pr.UpdatedBy, prDoc.UpdatedBy)
	}
}

func (suite *RwApiTestSuite) TestReindexIssuesWithDeleted() {
	t := suite.T()
	ctx := context.Background()

	// cleanup index
	suite.OpensearchKit.RecreateIndices(ctx)

	// disable Opensearch to prevent entities indexing
	suite.cfg.OpenSearch.Enabled = false

	repo := suite.repos.TreeDiff
	alphaRepo := suite.repos.Alpha
	suite.createIssuesForReindex(repo)
	suite.createIssuesForReindex(alphaRepo)

	err := suite.IssueService.DeleteByRepo(ctx, alphaRepo.ID, suite.users.Admin.ID, entities.NotifyOptions{})
	require.NoError(t, err)

	suite.cfg.OpenSearch.Enabled = true

	// execute workflow
	execution, err := suite.Scheduler.ExecuteWorkflow(
		ctx,
		index.ReindexWorkflowID("reindex"),
		entities.WorkflowTypes.SearchReindexWorkflow,
		index.ReindexWorkflowParams{
			Entities:       []revision.EntityType{revision.EntityTypes.Issue},
			IdempotencyKey: "reindex",
		})
	require.NoError(t, err)

	err = execution.Get(ctx, nil)
	require.NoError(t, err)

	err = suite.OpensearchKit.RefreshIndex(ctx)
	require.NoError(t, err)

	// check issues are in index
	hits, err := suite.OpensearchKit.GetDocumentsByType(ctx, mappings.IssueMappingType)
	require.NoError(t, err)

	docs := functools.SliceToMapKV(hits, func(hit json.RawMessage) (uint64, *mappings.IssueDocument) {
		typeDoc := TypeWrapper{}
		err := json.Unmarshal(hit, &typeDoc)
		require.NoError(t, err)
		doc := mappings.IssueDocument{}
		err = json.Unmarshal(hit, &doc)
		require.NoError(t, err)
		return doc.ID, &doc
	})

	res, err := suite.IssueRepo.List(ctx, interfaces.IssueListArgs{WithDeletedIssues: true}, pagination.Options{})
	require.NoError(t, err)
	for _, issue := range res.Result {
		if issue.IsDeleted {
			// check that there are no deleted issues in docs from index
			_, ok := docs[issue.ID]
			require.False(t, ok)
			continue
		}
		issueDoc, ok := docs[issue.ID]
		require.True(t, ok)
		require.Equal(t, issue.PublicID, issueDoc.PublicID)
		require.Equal(t, strconv.FormatUint(issue.PublicID, 10), issueDoc.PublicIDKeyword)
		require.Equal(t, issue.Title, issueDoc.Title)
		require.Equal(t, issue.Description, issueDoc.Description)
		require.Equal(t, string(issue.Visibility), issueDoc.Visibility)
		require.Equal(t, issue.AssigneeID, issueDoc.AssigneeID)
		require.Equal(t, issue.AuthorID, issueDoc.AuthorID)
		require.Equal(t, issue.UpdatedBy, issueDoc.UpdatedBy)
	}
}

func (suite *RwApiTestSuite) TestReindexUsersWithDeleted() {
	t := suite.T()
	ctx := context.Background()

	suite.OpensearchKit.RecreateIndices(ctx)

	// Create test users
	activeUser1 := suite.RandomUserFixture()
	activeUser2 := suite.RandomUserFixture()
	deletedUser1 := suite.RandomUserFixture()
	deletedUser2 := suite.RandomUserFixture()

	// Wait for indexing to complete and verify all users are in the index
	suite.WaitForWorkflows(t, entities.WorkflowTypes.SearchIndexWorkflow)
	err := suite.OpensearchKit.RefreshIndex(ctx)
	require.NoError(t, err)

	// Verify all users are initially in index
	hitsInitial, err := suite.OpensearchKit.GetDocumentsByType(ctx, mappings.UserMappingType)
	require.NoError(t, err)
	docsInitial := functools.SliceToMapKV(hitsInitial, func(hit json.RawMessage) (uint64, *mappings.UserDocument) {
		typeDoc := TypeWrapper{}
		err := json.Unmarshal(hit, &typeDoc)
		require.NoError(t, err)
		doc := mappings.UserDocument{}
		err = json.Unmarshal(hit, &doc)
		require.NoError(t, err)
		return doc.ID, &doc
	})

	_, ok := docsInitial[activeUser1.ID]
	require.True(t, ok, "Active user 1 should be in index before reindex")
	_, ok = docsInitial[activeUser2.ID]
	require.True(t, ok, "Active user 2 should be in index before reindex")
	_, ok = docsInitial[deletedUser1.ID]
	require.True(t, ok, "User 1 should be in index before deletion")
	_, ok = docsInitial[deletedUser2.ID]
	require.True(t, ok, "User 2 should be in index before deletion")

	// Disable OpenSearch to prevent automatic indexing during deletion
	suite.cfg.OpenSearch.Enabled = false

	// Remove memberships before deleting users (to avoid FK constraint violations)
	orgs1, err := suite.MembershipRepo.ListOrganizations(ctx, deletedUser1.Identity)
	require.NoError(t, err)
	for _, orgIdentity := range orgs1 {
		err = suite.MembershipRepo.Delete(ctx, deletedUser1.Identity, *orgIdentity)
		require.NoError(t, err)
	}

	orgs2, err := suite.MembershipRepo.ListOrganizations(ctx, deletedUser2.Identity)
	require.NoError(t, err)
	for _, orgIdentity := range orgs2 {
		err = suite.MembershipRepo.Delete(ctx, deletedUser2.Identity, *orgIdentity)
		require.NoError(t, err)
	}

	// Delete some users (soft delete) and advance their revisions
	err = suite.UserRepo.Delete(ctx, deletedUser1.ID)
	require.NoError(t, err)
	_, _, err = suite.RevRepo.AdvanceRevision(ctx, revision.User(deletedUser1.ID).Self)
	require.NoError(t, err)

	err = suite.UserRepo.Delete(ctx, deletedUser2.ID)
	require.NoError(t, err)
	_, _, err = suite.RevRepo.AdvanceRevision(ctx, revision.User(deletedUser2.ID).Self)
	require.NoError(t, err)

	// Re-enable OpenSearch
	suite.cfg.OpenSearch.Enabled = true

	// Execute reindex workflow
	execution, err := suite.Scheduler.ExecuteWorkflow(
		ctx,
		index.ReindexWorkflowID("reindex"),
		entities.WorkflowTypes.SearchReindexWorkflow,
		index.ReindexWorkflowParams{
			Entities:       []revision.EntityType{revision.EntityTypes.User},
			IdempotencyKey: "reindex",
		})
	require.NoError(t, err)

	err = execution.Get(ctx, nil)
	require.NoError(t, err)

	err = suite.OpensearchKit.RefreshIndex(ctx)
	require.NoError(t, err)

	// Check users in index after reindex
	hits, err := suite.OpensearchKit.GetDocumentsByType(ctx, mappings.UserMappingType)
	require.NoError(t, err)

	docs := functools.SliceToMapKV(hits, func(hit json.RawMessage) (uint64, *mappings.UserDocument) {
		typeDoc := TypeWrapper{}
		err := json.Unmarshal(hit, &typeDoc)
		require.NoError(t, err)
		doc := mappings.UserDocument{}
		err = json.Unmarshal(hit, &doc)
		require.NoError(t, err)
		return doc.ID, &doc
	})

	// Check deleted users are NOT in index
	_, ok = docs[deletedUser1.ID]
	require.False(t, ok, "Deleted user 1 should not be in index")
	_, ok = docs[deletedUser2.ID]
	require.False(t, ok, "Deleted user 2 should not be in index")

	// Check active users ARE in index
	userDoc1, ok := docs[activeUser1.ID]
	require.True(t, ok, "Active user 1 should be in index")
	require.Equal(t, activeUser1.Username, userDoc1.Username)
	require.Equal(t, activeUser1.DisplayName, userDoc1.PublicName)
	require.Equal(t, activeUser1.Email, userDoc1.Email)

	userDoc2, ok := docs[activeUser2.ID]
	require.True(t, ok, "Active user 2 should be in index")
	require.Equal(t, activeUser2.Username, userDoc2.Username)
	require.Equal(t, activeUser2.DisplayName, userDoc2.PublicName)
	require.Equal(t, activeUser2.Email, userDoc2.Email)
}

func (suite *RwApiTestSuite) TestReindexPRsWithDeleted() {
	t := suite.T()
	ctx := context.Background()

	// cleanup index
	suite.OpensearchKit.RecreateIndices(ctx)

	// disable Opensearch to prevent entities indexing
	suite.cfg.OpenSearch.Enabled = false

	repo1 := suite.repos.TreeDiff
	repo2 := suite.repos.PathInfo
	suite.createPrsForReindex(repo1)
	suite.createPrsForReindex(repo2)

	err := suite.PullRequestService.DeleteByRepo(ctx, repo1.ID)
	require.NoError(t, err)

	suite.cfg.OpenSearch.Enabled = true

	// execute workflow
	execution, err := suite.Scheduler.ExecuteWorkflow(
		ctx,
		index.ReindexWorkflowID("reindex"),
		entities.WorkflowTypes.SearchReindexWorkflow,
		index.ReindexWorkflowParams{
			Entities:       []revision.EntityType{revision.EntityTypes.PullRequest},
			IdempotencyKey: "reindex",
		})
	require.NoError(t, err)

	err = execution.Get(ctx, nil)
	require.NoError(t, err)

	err = suite.OpensearchKit.RefreshIndex(ctx)
	require.NoError(t, err)

	// check prs are in index
	hits, err := suite.OpensearchKit.GetDocumentsByType(ctx, mappings.PullRequestMappingType)
	require.NoError(t, err)

	docs := functools.SliceToMapKV(hits, func(hit json.RawMessage) (uint64, *mappings.PullRequestDocument) {
		typeDoc := TypeWrapper{}
		err := json.Unmarshal(hit, &typeDoc)
		require.NoError(t, err)
		doc := mappings.PullRequestDocument{}
		err = json.Unmarshal(hit, &doc)
		require.NoError(t, err)
		return doc.ID, &doc
	})

	res, err := suite.PullRequestRepo.List(ctx, interfaces.PullRequestListArgs{WithDeletedRepos: true}, pagination.Options{})
	require.NoError(t, err)
	for _, pr := range res.Result {
		if pr.IsDeleted != nil && *pr.IsDeleted {
			// check that there are no deleted prs in docs from index
			_, ok := docs[pr.ID]
			require.False(t, ok)
			continue
		}
		prDoc, ok := docs[pr.ID]
		require.True(t, ok)
		require.Equal(t, pr.PublicID, prDoc.PublicID)
		require.Equal(t, strconv.FormatUint(pr.PublicID, 10), prDoc.PublicIDKeyword)
		require.Equal(t, pr.RepoID, prDoc.RepoID)
		require.Equal(t, pr.Title, prDoc.Title)
		require.Equal(t, pr.Description, prDoc.Description)
		require.Equal(t, pr.AuthorID, prDoc.AuthorID)
		require.Equal(t, pr.UpdatedBy, prDoc.UpdatedBy)
	}
}

func (suite *RwApiTestSuite) TestReindexWorkflow_Housekeeping() {
	t := suite.T()
	ctx := context.Background()

	// cleanup index
	suite.OpensearchKit.RecreateIndices(ctx)

	// disable Opensearch to prevent entities indexing
	suite.cfg.OpenSearch.Enabled = false

	repo := suite.repos.TreeDiff
	suite.createIssuesForReindex(repo)
	suite.createPrsForReindex(repo)

	suite.cfg.OpenSearch.Enabled = true

	task := housekeeping.NewStartOpensearchReindexWorkflowCmd(housekeeping.StartOpensearchReindexWorkflowCmdDeps{
		Scheduler:     suite.Scheduler,
		IndexServices: suite.IndexServices,
		Cfg:           suite.cfg,
	})

	err := task.Invoke("-entities", "user,issue,pull_request")
	require.NoError(t, err)

	suite.WaitForWorkflows(t, entities.WorkflowTypes.SearchReindexWorkflow)

	task = housekeeping.NewCancelOpensearchReindexWorkflowCmd(housekeeping.CancelOpensearchReindexWorkflowCmdDeps{
		Scheduler: suite.Scheduler,
	})
	err = task.Invoke()
	require.NoError(t, err)
}

func (suite *RwApiTestSuite) createIssuesForReindex(repo *entities.Repository) {
	issuesNum := 14
	barash, krosh := suite.users.Barash, suite.users.Krosh
	var author *entities.User

	for i := 0; i < issuesNum; i++ {
		rv := rand.Intn(2)
		if rv == 0 {
			author = barash
		} else {
			author = krosh
		}

		suite.makeIssue(author,
			&makeIssueOptions{
				RepoID:      repo.ID,
				Title:       "Task" + strconv.Itoa(i),
				Description: "Simple task " + strconv.Itoa(i),
				Priority:    entities.IssuePriorities.Normal,
				Visibility:  entities.IssueVisibilities.Public,
			})
	}
}

func (suite *RwApiTestSuite) createPrsForReindex(repo *entities.Repository) {
	prsNum := 13

	barash, krosh := suite.users.Barash, suite.users.Krosh

	barashCg := suite.setupRepo(repo, barash)
	kroshCg := suite.setupRepo(repo, krosh)
	var author *entities.User
	var authorCg cgit.CGit

	for i := 0; i < prsNum; i++ {
		rv := rand.Intn(2)
		if rv == 0 {
			author = barash
			authorCg = barashCg
		} else {
			author = krosh
			authorCg = kroshCg
		}
		suite.createPr(repo, "feature-reindex-"+strconv.Itoa(i), "Title for pr "+strconv.Itoa(i),
			"Desc for pr "+strconv.Itoa(i), author, authorCg)
	}

}
