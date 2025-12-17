package integrationtests

import (
	commongrpc "common/grpc"
	temporal_errors "common/temporal/errors"
	"common/utils"
	"context"
	goerrors "errors"
	"fmt"
	"gitcore/internal/entities"
	except "gitcore/internal/exceptions"
	"gitcore/internal/interfaces"
	"gitcore/internal/migrations"
	migration_activities "gitcore/internal/migrations/activities"
	"gitcore/internal/migrations/stub"
	"gitcore/internal/testutils"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"strings"
	"testing"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.temporal.io/api/enums/v1"
	"go.uber.org/fx"
)

type MockAuthenticator struct {
	identity *entities.UserIdentity
	token    string
}

func NewMockAuthenticator(identity *entities.UserIdentity, token string) *MockAuthenticator {
	return &MockAuthenticator{
		identity: identity,
		token:    token,
	}
}

func (m *MockAuthenticator) Subject() *entities.UserIdentity {
	return m.identity
}

func (m *MockAuthenticator) AuthenticateGrpcRequest(ctx context.Context) (context.Context, error) {
	return ctx, nil
}

func (m *MockAuthenticator) MarshalToStruct() *entities.MarshalledAuthenticator {
	return &entities.MarshalledAuthenticator{
		Type:     entities.AuthenticatorTypes.IAM,
		Identity: m.identity,
		Token:    m.token,
	}
}

type MockMigrator struct {
	mockRepo  *stub.StubExternalRepository
	opService interfaces.OperationService
}

func NewMockMigrator(mockRepo *stub.StubExternalRepository, opService interfaces.OperationService) *MockMigrator {
	return &MockMigrator{
		mockRepo:  mockRepo,
		opService: opService,
	}
}

func (m *MockMigrator) GetRepository(sourceURL string) (interfaces.ExternalRepository, error) {
	return m.mockRepo, nil
}

func (m *MockMigrator) GetRepositoryWithAuth(ctx context.Context, URL string, credentials entities.Credentials) (interfaces.ExternalRepository, error) {
	return m.mockRepo, nil
}

func (m *MockMigrator) CreateMigrationOperation(ctx context.Context, user *entities.User, repoID uint64) (*entities.Operation, error) {
	operationID := migrations.UnittestPrefix + uuid.NewString()[:8]

	operation := &entities.Operation{
		ID:     operationID,
		Type:   entities.OperationTypes.Migration,
		Status: entities.OperationStatuses.Scheduled,
		UserID: user.ID,
		IamObject: entities.IAMObject{
			Type: entities.ObjectTypes.Repository,
			ID:   repoID,
		},
	}

	_, err := m.opService.Create(ctx, operation)
	if err != nil {
		return nil, err
	}
	return operation, nil
}

func (m *MockMigrator) GetCredentials(ctx context.Context, repoID uint64) (entities.Credentials, error) {
	return entities.Credentials{}, nil
}

func (m *MockMigrator) StoreCredentials(ctx context.Context, repoID uint64, credentials *entities.Credentials) (string, error) {
	return "", nil
}

func (m *MockMigrator) UpdateCredentials(ctx context.Context, repoID uint64, credentials *entities.Credentials) (string, error) {
	return "operation-id", nil
}

type MockMigrationTestSuite struct {
	IntegrationTestSuite
	mockMigrator *MockMigrator
}

func (suite *MockMigrationTestSuite) SetupSuite() {
	suite.SetupFlags()
	suite.SetupServer(
		fx.Decorate(func(service interfaces.OperationService) interfaces.Migrator {
			suite.mockMigrator = NewMockMigrator(nil, service)
			return suite.mockMigrator
		}),
	)
	suite.SetupDefaultQuotaLimits()
	suite.SetupUsers(Create)
	suite.SetupOrganizations(Create)
	suite.SetupMockMigrationRepo(Create)
	suite.SetupTemporalAttempts(1)
}

func (suite *MockMigrationTestSuite) TearDownSuite() {
	suite.IntegrationTestSuite.TearDownSuite()
	suite.CancelAllWorkflows()
}

func TestMockMigrationTestSuite(t *testing.T) {
	suite.Run(t, new(MockMigrationTestSuite))
}

type migrationWorkspace struct {
	stubRepo           *stub.StubExternalRepository
	defaultOrg         *entities.Organization
	defaultUser        *entities.User
	defaultCredentials entities.Credentials
	authenticator      interfaces.Authenticator
}

func (suite *MockMigrationTestSuite) migrate(t *testing.T, w *migrationWorkspace, repoSlug string) *entities.Operation {
	operation, err := suite.MigrationService.Migrate(
		context.Background(),
		w.defaultUser,
		w.defaultOrg,
		w.stubRepo.URL(),
		w.defaultCredentials,
		repoSlug,
		nil,
		w.authenticator,
		entities.Visibilities.Public,
		false,
		nil,
	)
	require.NoError(t, err)
	require.NotNil(t, operation)
	return operation
}

func (suite *MockMigrationTestSuite) prepareWorkspace() *migrationWorkspace {
	url := suite.URL(suite.repos.MockMigration)

	stubRepo := stub.NewStubExternalRepository(
		entities.MigrationSourceGithub,
		"src-migration-tests/generic",
		"generic",
		url,
		url,
		"github.com",
		map[entities.MigrationProviderFeature]bool{
			entities.MigrationFeatureTokenCredentials: true,
		},
	)
	// Update the mockMigrator with the new stubRepo
	suite.mockMigrator.mockRepo = stubRepo

	user := suite.UserFixture(entities.UserIdentity{
		ID:  fmt.Sprintf("test-user-%s", uuid.NewString()),
		Src: entities.IdentityProviders.IAM,
	}, entities.Visibilities.Public)

	orgSlug := uuid.NewString()
	org := suite.OrganizationFixture(Create, orgSlug, user, nil)

	return &migrationWorkspace{
		stubRepo:    stubRepo,
		defaultOrg:  org,
		defaultUser: user,
		defaultCredentials: entities.Credentials{
			Token: utils.PtrFromValue("test-token"),
		},
		authenticator: suite.getFakeAuthenticator(user.Identity),
	}
}

func getMetadata(t *testing.T, operation *entities.Operation) *pb.MigrationMetadata {
	metadata := new(pb.MigrationMetadata)
	err := operation.GetMetadata(metadata)
	require.NoError(t, err)
	return metadata
}

func (suite *MockMigrationTestSuite) TestStubMigrate() {
	t := suite.T()
	ctx := context.Background()

	repoSlug := "test-repo"
	workspace := suite.prepareWorkspace()

	// test

	operation := suite.migrate(t, workspace, "test-repo")
	require.Equal(t, entities.OperationTypes.Migration, operation.Type)
	require.Equal(t, entities.OperationStatuses.Scheduled, operation.Status)

	repo, err := suite.RepoRepo.GetRepositoryByOrgID(ctx, workspace.defaultOrg.ID, repoSlug)
	require.NoError(t, err)

	require.Equal(t, repoSlug, repo.Slug)
	require.Equal(t, workspace.defaultOrg.ID, repo.OrgID)
	require.Equal(t, workspace.defaultUser.ID, repo.CreatedBy)
	require.Equal(t, entities.Visibilities.Public, repo.Visibility)
	require.NotNil(t, repo.MigrationID)
	require.Equal(t, operation.ID, strings.TrimSpace(*repo.MigrationID), "Repository migration ID should match operation ID")

	t.Log("Waiting for migration workflow to complete...")
	err = suite.MigrationService.WaitForMigration(ctx, operation.ID)
	require.NoError(t, err, "Migration workflow should complete without errors")

	// verify

	updatedOperation, err := suite.OpService.Get(ctx, operation.ID)
	require.NoError(t, err)
	require.Equal(t, entities.OperationStatuses.Success, updatedOperation.Status, "Operation status should be Success")
}

func (suite *MockMigrationTestSuite) TestMigrateWithIssuesError() {
	t := suite.T()
	ctx := context.Background()

	workspace := suite.prepareWorkspace()
	// Set an error to be emitted during issues iteration
	workspace.stubRepo.SetIssuesIterator(func(yield func(interfaces.ExternalIssue, error) bool) {
		yield(nil, errors.New("test error during issues iteration"))
	})

	operation := suite.migrate(t, workspace, "test-repo-with-error")
	require.Equal(t, entities.OperationTypes.Migration, operation.Type)
	require.Equal(t, entities.OperationStatuses.Scheduled, operation.Status)

	t.Log("Waiting for migration workflow to complete...")
	err := suite.MigrationService.WaitForMigration(ctx, operation.ID)
	require.NoError(t, err)

	updatedOperation, err := suite.OpService.Get(ctx, operation.ID)
	require.NoError(t, err)

	require.Equal(t, entities.OperationStatuses.Success, updatedOperation.Status)

	metadata := getMetadata(t, updatedOperation)

	for _, step := range metadata.Steps {
		if step.Step == pb.Step_STEP_ISSUES {
			require.Equal(t, step.Status, pb.MigrationStatus_STATUS_FAILED)
		}
	}
	require.Nil(t, metadata.FatalError)
}

func (suite *MockMigrationTestSuite) TestCloneError() {
	t := suite.T()
	ctx := context.Background()

	workspace := suite.prepareWorkspace()
	// to cause error during 'push'
	cancel := suite.setQuotaLimit(t, workspace.defaultOrg.ID, entities.Quotas.ObjectStorageSize, 1)
	defer cancel()

	// test

	operation := suite.migrate(t, workspace, "test-repo-with-error")
	require.Equal(t, entities.OperationTypes.Migration, operation.Type)
	require.Equal(t, entities.OperationStatuses.Scheduled, operation.Status)

	t.Log("Waiting for migration workflow to complete...")
	err := suite.MigrationService.WaitForMigration(ctx, operation.ID)
	require.NoError(t, err)

	// check that fatal error is indeed QuotaLimitExceeded
	t.Run("is stored as fatal error", func(t *testing.T) {
		updatedOperation, err := suite.OpService.Get(ctx, operation.ID)
		require.NoError(t, err, "Should be able to get the updated operation")
		require.Equal(t, entities.OperationStatuses.Failed, updatedOperation.Status)

		metadata := getMetadata(t, updatedOperation)

		require.NotNil(t, metadata.FatalError)
		require.Equal(t, except.QuotaLimitExceeded.MessageID, metadata.FatalError.MessageId)
	})

	// check that QuotaLimit worked as a non-retryable error
	t.Run("is non-retryable", func(t *testing.T) {
		workflowID := fmt.Sprintf("op:%s:migration", operation.ID) // hard-coded as in gitcore/internal/migrations/service.go:671
		events := suite.Scheduler.GetWorkflowEvents(ctx, workflowID, "", true)
		cloneActivityRuns := 0

		for events.HasNext() {
			event, err := events.Next()
			require.NoError(t, err)

			if event.EventType != enums.EVENT_TYPE_ACTIVITY_TASK_SCHEDULED {
				continue
			}
			details := event.GetActivityTaskScheduledEventAttributes()
			if details.ActivityType.Name == migration_activities.CloneRepository {
				cloneActivityRuns++
			}
		}

		require.Equal(t, 1, cloneActivityRuns)
	})
}

func (suite *MockMigrationTestSuite) TestMigratedRepoRating() {
	t := suite.T()
	ctx := context.Background()

	workspace := suite.prepareWorkspace()
	workspace.stubRepo.SetStars(10)
	// we want the migration to fail, so we can restart it later
	workspace.stubRepo.SetIssuesIterator(func(yield func(interfaces.ExternalIssue, error) bool) {
		yield(nil, errors.New("test error"))
	})

	var repo *entities.Repository
	checkRating := func(t *testing.T, expectedRating float64) {
		rating, err := suite.RatingRepo.Get(ctx, repo.ID)
		require.NoError(t, err)
		require.Equal(t, expectedRating, rating.Rating)
	}

	t.Log("Starting first migration...")
	operation := suite.migrate(t, workspace, "test-repo-with-error")
	require.Equal(t, entities.OperationTypes.Migration, operation.Type)
	require.Equal(t, entities.OperationStatuses.Scheduled, operation.Status)

	t.Log("Waiting for migration workflow to complete...")
	err := suite.MigrationService.WaitForMigration(ctx, operation.ID)
	require.NoError(t, err)

	operation, err = suite.OpService.Get(ctx, operation.ID)
	require.NoError(t, err)
	metadata := getMetadata(t, operation)
	repoID, err := commongrpc.ParseID(metadata.RepoId)
	require.NoError(t, err)
	repo, err = suite.RepoRepo.GetRepositoryByID(ctx, repoID)
	require.NoError(t, err)

	checkRating(t, 15.0)

	t.Log("Adding manual rating reaction...")
	_, err = pb.NewRatingServiceClient(suite.grpcClient).
		RateRepo(testutils.AuthorizeGRPC(workspace.defaultUser.Identity), &pb.RateRepoRequest{
			RepoId:   metadata.RepoId,
			Reaction: pb.RatingReactionType_RR_TYPE_POSITIVE_LEVEL_30,
		})
	require.NoError(t, err)
	checkRating(t, 20.0)

	t.Log("Restarting migration...")
	workspace.stubRepo.SetStars(500)
	newOperation, err := suite.MigrationService.Restart(
		ctx,
		workspace.defaultUser,
		repo,
		workspace.stubRepo.URL(),
		workspace.defaultCredentials,
		false,
		workspace.authenticator,
	)
	require.NoError(t, err)

	t.Log("Waiting for migration workflow to complete...")
	err = suite.MigrationService.WaitForMigration(ctx, newOperation.ID)
	require.NoError(t, err)
	checkRating(t, 755.0)

	t.Log("Running mirroring workflow...")
	workspace.stubRepo.SetStars(1000)
	_, err = suite.Scheduler.ExecuteWorkflow(
		ctx,
		fmt.Sprintf("mirror:%s", uuid.NewString()),
		entities.WorkflowTypes.StartMigrateRepo,
		migration_activities.MigrationParams{
			OrganizationID:     repo.OrgID,
			RepositoryID:       repo.ID,
			RepositorySlug:     repo.Slug,
			OrganizationSlug:   repo.OrgSlug,
			URL:                workspace.stubRepo.URL(),
			Visibility:         entities.Visibilities.Public,
			UserID:             workspace.defaultUser.ID,
			SourceCredentials:  workspace.defaultCredentials,
			Mirror:             true,
			IsMirroringCronJob: true,
		},
	)
	require.NoError(t, err)
	suite.WaitForWorkflows(t, entities.WorkflowTypes.StartMigrateRepo)
	checkRating(t, 1505.0)
}

func (suite *MockMigrationTestSuite) TestActivityErrorMarshalling() {
	t := suite.T()
	ctx := context.Background()

	for _, tc := range []struct {
		name    string
		prepare func(w *migrationWorkspace)
	}{
		{
			name: "issues",
			prepare: func(w *migrationWorkspace) {
				w.stubRepo.SetIssuesIterator(func(yield func(interfaces.ExternalIssue, error) bool) {
					yield(nil, goerrors.New("basic error"))
				})
			},
		},
		{
			name: "labels",
			prepare: func(w *migrationWorkspace) {
				w.stubRepo.SetLabelsIterator(func(yield func(interfaces.ExternalLabel, error) bool) {
					yield(nil, goerrors.New("basic error"))
				})
			},
		},
		{
			name: "milestones",
			prepare: func(w *migrationWorkspace) {
				w.stubRepo.SetMilestonesIterator(func(yield func(interfaces.ExternalMilestone, error) bool) {
					yield(nil, goerrors.New("basic error"))
				})
			},
		},
		{
			name: "pull requests",
			prepare: func(w *migrationWorkspace) {
				w.stubRepo.SetPullRequestsIterator(func(yield func(interfaces.ExternalPullRequest, error) bool) {
					yield(nil, goerrors.New("basic error"))
				})
			},
		},
		{
			name: "issue comments",
			prepare: func(w *migrationWorkspace) {
				w.stubRepo.SetIssueCommentsIterator(func(yield func(interfaces.ExternalIssueComment, error) bool) {
					yield(nil, goerrors.New("basic error"))
				})
			},
		},
		{
			name: "pull request comments",
			prepare: func(w *migrationWorkspace) {
				w.stubRepo.SetPullRequestCommentsIterator(func(yield func(interfaces.ExternalPullRequestComment, error) bool) {
					yield(nil, goerrors.New("basic error"))
				})
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workspace := suite.prepareWorkspace()

			cancel := temporal_errors.ToggleErrorMarshallingDebug()
			defer cancel()
			tc.prepare(workspace)

			operation := suite.migrate(t, workspace, "test-repo-with-activity-error")
			err := suite.MigrationService.WaitForMigration(ctx, operation.ID)
			require.NoError(t, err)
		})
	}
}

func (suite *MockMigrationTestSuite) TestRepoIsDeleted() {
	t := suite.T()
	ctx := context.Background()

	cancel := temporal_errors.ToggleErrorMarshallingDebug()
	defer cancel()

	t.Run("deleted during access check", func(t *testing.T) {
		workspace := suite.prepareWorkspace()
		workspace.stubRepo.SetIssuesIterator(func(yield func(interfaces.ExternalIssue, error) bool) {
			time.Sleep(20 * time.Second)
		})

		operation := suite.migrate(t, workspace, "test-deleted")
		metadata := getMetadata(t, operation)

		_, err := pb.NewRepoServiceClient(suite.grpcClient).Delete(
			testutils.AuthorizeGRPC(workspace.defaultUser.Identity),
			&pb.DeleteRepositoryRequest{
				Id: metadata.RepoId,
			},
		)
		require.NoError(t, err)

		err = suite.MigrationService.WaitForMigration(ctx, operation.ID)
		require.NoError(t, err)
	})

	t.Run("from mirroring, default", func(t *testing.T) {
		workspace := suite.prepareWorkspace()
		operation := suite.migrate(t, workspace, "test-deleted")
		metadata := getMetadata(t, operation)
		err := suite.MigrationService.WaitForMigration(ctx, operation.ID)
		require.NoError(t, err)

		repoID, err := commongrpc.ParseID(metadata.RepoId)
		require.NoError(t, err)
		repo, err := suite.RepoRepo.GetRepositoryByID(ctx, repoID)
		require.NoError(t, err)

		err = suite.RepoService.DeleteRepository(ctx, repo.ID, suite.users.Admin.ID)
		require.NoError(t, err)

		_, err = suite.Scheduler.ExecuteWorkflow(
			ctx,
			fmt.Sprintf("mirror:%s", uuid.NewString()),
			entities.WorkflowTypes.StartMigrateRepo,
			migration_activities.MigrationParams{
				OrganizationID:     repo.OrgID,
				RepositoryID:       repo.ID,
				RepositorySlug:     repo.Slug,
				OrganizationSlug:   repo.OrgSlug,
				URL:                workspace.stubRepo.URL(),
				Visibility:         entities.Visibilities.Public,
				UserID:             workspace.defaultUser.ID,
				SourceCredentials:  workspace.defaultCredentials,
				Mirror:             true,
				IsMirroringCronJob: true,
			},
		)
		require.NoError(t, err)
		suite.WaitForWorkflows(t, entities.WorkflowTypes.StartMigrateRepo)

		migratedRepo, err := suite.MigratedRepoRepo.Get(ctx, repo.ID)
		require.NoError(t, err)
		require.NotNil(t, migratedRepo.LastMirroringID)
		mirrorOp, err := suite.OpRepo.Get(ctx, *migratedRepo.LastMirroringID)
		require.NoError(t, err)

		metadata = getMetadata(t, mirrorOp)
		require.NotNil(t, metadata.FatalError)
		require.Equal(t, except.RepositoryNotFound.MessageID, metadata.FatalError.MessageId)
	})

	t.Run("from mirroring, error from secrets service", func(t *testing.T) {
		workspace := suite.prepareWorkspace()
		operation := suite.migrate(t, workspace, "test-deleted")
		metadata := getMetadata(t, operation)
		err := suite.MigrationService.WaitForMigration(ctx, operation.ID)
		require.NoError(t, err)

		repoID, err := commongrpc.ParseID(metadata.RepoId)
		require.NoError(t, err)
		repo, err := suite.RepoRepo.GetRepositoryByID(ctx, repoID)
		require.NoError(t, err)

		err = suite.RepoService.DeleteRepository(ctx, repo.ID, suite.users.Admin.ID)
		require.NoError(t, err)

		st, err := except.RepositoryNotFound.BuildNoStack(repo.ID).AsStatus()
		require.NoError(t, err)
		suite.secretService.EmulateError(st.Err()) // it's what comes out of middleware
		defer func() {
			suite.secretService.EmulateError(nil)
		}()

		_, err = suite.Scheduler.ExecuteWorkflow(
			ctx,
			fmt.Sprintf("mirror:%s", uuid.NewString()),
			entities.WorkflowTypes.StartMigrateRepo,
			migration_activities.MigrationParams{
				OrganizationID:   repo.OrgID,
				RepositoryID:     repo.ID,
				RepositorySlug:   repo.Slug,
				OrganizationSlug: repo.OrgSlug,
				URL:              workspace.stubRepo.URL(),
				Visibility:       entities.Visibilities.Public,
				UserID:           workspace.defaultUser.ID,
				// SourceCredentials:  workspace.defaultCredentials,
				SecretOperationID:  "something", // force to fetch secret from SecretService
				Mirror:             true,
				IsMirroringCronJob: true,
			},
		)
		require.NoError(t, err)
		suite.WaitForWorkflows(t, entities.WorkflowTypes.StartMigrateRepo)

		migratedRepo, err := suite.MigratedRepoRepo.Get(ctx, repo.ID)
		require.NoError(t, err)
		require.NotNil(t, migratedRepo.LastMirroringID)
		mirrorOp, err := suite.OpRepo.Get(ctx, *migratedRepo.LastMirroringID)
		require.NoError(t, err)

		metadata = getMetadata(t, mirrorOp)
		require.NotNil(t, metadata.FatalError)
		require.Equal(t, except.RepositoryNotFound.MessageID, metadata.FatalError.MessageId)
	})
}
