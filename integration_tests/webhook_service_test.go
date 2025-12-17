package integrationtests

import (
	"common/utils"
	"context"
	"gitcore/internal/entities"

	"github.com/stretchr/testify/require"
)

const (
	MaxAttemptCountForWebhookFailure = 3
)

func (suite *RwApiTestSuite) TestWebhookService_CountConsecutiveFailures_AllFailures() {
	t := suite.T()
	ctx := context.Background()

	repoID, _, _ := suite.makeRandomRepo(t, suite.users.Kopatych)

	// Create a webhook
	webhook := &entities.Webhook{
		Slug:            "test-webhook-failures",
		Name:            utils.PtrFromValue("Test Webhook"),
		EntityType:      entities.EntityTypes.Repository,
		EntityID:        repoID,
		PayloadURL:      "https://example.com/webhook",
		Status:          entities.WebhookStatuses.Active,
		SSLVerification: true,
		Events:          entities.WebhookEvents{"*"},
	}

	webhookID, err := suite.WebhookRepo.Create(ctx, webhook)
	require.NoError(t, err)

	// Create 5 consecutive failed deliveries (with errors)
	for i := 0; i < 5; i++ {
		errorMsg := "Network timeout"
		statusCode := 500
		log := &entities.WebhookLog{
			WebhookID:    webhookID,
			Error:        &errorMsg,
			StatusCode:   &statusCode,
			AttemptCount: 3, // All attempts exhausted
		}
		_, err := suite.WebhookLogRepo.Create(ctx, log)
		require.NoError(t, err)
	}

	// Count consecutive failures
	count, err := suite.WebhookService.CountConsecutiveFailures(ctx, webhookID, MaxAttemptCountForWebhookFailure, 10)
	require.NoError(t, err)
	require.Equal(t, 5, count)
}

func (suite *RwApiTestSuite) TestWebhookService_CountConsecutiveFailures_SuccessBreaksStreak() {
	t := suite.T()
	ctx := context.Background()

	repoID, _, _ := suite.makeRandomRepo(t, suite.users.Kopatych)

	// Create a webhook
	webhook := &entities.Webhook{
		Slug:            "test-webhook-streak",
		Name:            utils.PtrFromValue("Test Webhook"),
		EntityType:      entities.EntityTypes.Repository,
		EntityID:        repoID,
		PayloadURL:      "https://example.com/webhook",
		Status:          entities.WebhookStatuses.Active,
		SSLVerification: true,
		Events:          entities.WebhookEvents{"*"},
	}

	webhookID, err := suite.WebhookRepo.Create(ctx, webhook)
	require.NoError(t, err)

	// Create older failures (should not be counted)
	for i := 0; i < 3; i++ {
		errorMsg := "Old failure"
		log := &entities.WebhookLog{
			WebhookID:    webhookID,
			Error:        &errorMsg,
			AttemptCount: 3,
		}
		_, err := suite.WebhookLogRepo.Create(ctx, log)
		require.NoError(t, err)
	}

	// Create a successful delivery
	statusCode := 200
	successLog := &entities.WebhookLog{
		WebhookID:    webhookID,
		StatusCode:   &statusCode,
		AttemptCount: 3,
	}
	_, err = suite.WebhookLogRepo.Create(ctx, successLog)
	require.NoError(t, err)

	// Create recent failures (only these should be counted)
	for i := 0; i < 2; i++ {
		errorMsg := "Recent failure"
		log := &entities.WebhookLog{
			WebhookID:    webhookID,
			Error:        &errorMsg,
			AttemptCount: 3,
		}
		_, err := suite.WebhookLogRepo.Create(ctx, log)
		require.NoError(t, err)
	}

	// Count consecutive failures - should only count the 2 most recent
	count, err := suite.WebhookService.CountConsecutiveFailures(ctx, webhookID, MaxAttemptCountForWebhookFailure, 10)
	require.NoError(t, err)
	require.Equal(t, 2, count)
}

func (suite *RwApiTestSuite) TestWebhookService_CountConsecutiveFailures_ErrorStatusCodes() {
	t := suite.T()
	ctx := context.Background()

	repoID, _, _ := suite.makeRandomRepo(t, suite.users.Kopatych)

	// Create a webhook
	webhook := &entities.Webhook{
		Slug:            "test-webhook-statuses",
		Name:            utils.PtrFromValue("Test Webhook"),
		EntityType:      entities.EntityTypes.Repository,
		EntityID:        repoID,
		PayloadURL:      "https://example.com/webhook",
		Status:          entities.WebhookStatuses.Active,
		SSLVerification: true,
		Events:          entities.WebhookEvents{"*"},
	}

	webhookID, err := suite.WebhookRepo.Create(ctx, webhook)
	require.NoError(t, err)

	// Create failures with different error status codes
	errorStatusCodes := []int{404, 500, 503, 400}
	for _, code := range errorStatusCodes {
		log := &entities.WebhookLog{
			WebhookID:    webhookID,
			StatusCode:   &code,
			AttemptCount: 3,
		}
		_, err := suite.WebhookLogRepo.Create(ctx, log)
		require.NoError(t, err)
	}

	// Count consecutive failures
	count, err := suite.WebhookService.CountConsecutiveFailures(ctx, webhookID, MaxAttemptCountForWebhookFailure, 10)
	require.NoError(t, err)
	require.Equal(t, 4, count)
}

func (suite *RwApiTestSuite) TestWebhookService_CountConsecutiveFailures_MixedErrorTypes() {
	t := suite.T()
	ctx := context.Background()

	repoID, _, _ := suite.makeRandomRepo(t, suite.users.Kopatych)

	// Create a webhook
	webhook := &entities.Webhook{
		Slug:            "test-webhook-mixed",
		Name:            utils.PtrFromValue("Test Webhook"),
		EntityType:      entities.EntityTypes.Repository,
		EntityID:        repoID,
		PayloadURL:      "https://example.com/webhook",
		Status:          entities.WebhookStatuses.Active,
		SSLVerification: true,
		Events:          entities.WebhookEvents{"*"},
	}

	webhookID, err := suite.WebhookRepo.Create(ctx, webhook)
	require.NoError(t, err)

	// Create failure with error message
	errorMsg1 := "Connection refused"
	log1 := &entities.WebhookLog{
		WebhookID:    webhookID,
		Error:        &errorMsg1,
		AttemptCount: 3,
	}
	_, err = suite.WebhookLogRepo.Create(ctx, log1)
	require.NoError(t, err)

	// Create failure with error status code
	statusCode := 500
	log2 := &entities.WebhookLog{
		WebhookID:    webhookID,
		StatusCode:   &statusCode,
		AttemptCount: 3,
	}
	_, err = suite.WebhookLogRepo.Create(ctx, log2)
	require.NoError(t, err)

	// Create failure with both error message and status code
	errorMsg2 := "Server error"
	statusCode2 := 503
	log3 := &entities.WebhookLog{
		WebhookID:    webhookID,
		Error:        &errorMsg2,
		StatusCode:   &statusCode2,
		AttemptCount: 3,
	}
	_, err = suite.WebhookLogRepo.Create(ctx, log3)
	require.NoError(t, err)

	// Count consecutive failures
	count, err := suite.WebhookService.CountConsecutiveFailures(ctx, webhookID, MaxAttemptCountForWebhookFailure, 10)
	require.NoError(t, err)
	require.Equal(t, 3, count)
}

func (suite *RwApiTestSuite) TestWebhookService_CountConsecutiveFailures_IgnoresIncompleteDeliveries() {
	t := suite.T()
	ctx := context.Background()

	repoID, _, _ := suite.makeRandomRepo(t, suite.users.Kopatych)

	// Create a webhook
	webhook := &entities.Webhook{
		Slug:            "test-webhook-incomplete",
		Name:            utils.PtrFromValue("Test Webhook"),
		EntityType:      entities.EntityTypes.Repository,
		EntityID:        repoID,
		PayloadURL:      "https://example.com/webhook",
		Status:          entities.WebhookStatuses.Active,
		SSLVerification: true,
		Events:          entities.WebhookEvents{"*"},
	}

	webhookID, err := suite.WebhookRepo.Create(ctx, webhook)
	require.NoError(t, err)

	// Create incomplete deliveries (attempt_count < 3) - should be ignored
	for i := 0; i < 2; i++ {
		errorMsg := "In progress"
		log := &entities.WebhookLog{
			WebhookID:    webhookID,
			Error:        &errorMsg,
			AttemptCount: 1, // Still retrying
		}
		_, err := suite.WebhookLogRepo.Create(ctx, log)
		require.NoError(t, err)
	}

	// Create completed failures (attempt_count = 3)
	for i := 0; i < 3; i++ {
		errorMsg := "Failed delivery"
		log := &entities.WebhookLog{
			WebhookID:    webhookID,
			Error:        &errorMsg,
			AttemptCount: 3, // Completed all retries
		}
		_, err := suite.WebhookLogRepo.Create(ctx, log)
		require.NoError(t, err)
	}

	// Count should only include completed deliveries (the 3 with attempt_count=3)
	// The 2 incomplete ones (attempt_count=1) should be skipped
	count, err := suite.WebhookService.CountConsecutiveFailures(ctx, webhookID, MaxAttemptCountForWebhookFailure, 10)
	require.NoError(t, err)
	require.Equal(t, 3, count)
}

func (suite *RwApiTestSuite) TestWebhookService_CountConsecutiveFailures_RespectsLimit() {
	t := suite.T()
	ctx := context.Background()

	repoID, _, _ := suite.makeRandomRepo(t, suite.users.Kopatych)

	// Create a webhook
	webhook := &entities.Webhook{
		Slug:            "test-webhook-limit",
		Name:            utils.PtrFromValue("Test Webhook"),
		EntityType:      entities.EntityTypes.Repository,
		EntityID:        repoID,
		PayloadURL:      "https://example.com/webhook",
		Status:          entities.WebhookStatuses.Active,
		SSLVerification: true,
		Events:          entities.WebhookEvents{"*"},
	}

	webhookID, err := suite.WebhookRepo.Create(ctx, webhook)
	require.NoError(t, err)

	// Create 10 consecutive failures
	for i := 0; i < 10; i++ {
		errorMsg := "Failure"
		log := &entities.WebhookLog{
			WebhookID:    webhookID,
			Error:        &errorMsg,
			AttemptCount: 3,
		}
		_, err := suite.WebhookLogRepo.Create(ctx, log)
		require.NoError(t, err)
	}

	// Count with limit of 5 - should only check last 5
	count, err := suite.WebhookService.CountConsecutiveFailures(ctx, webhookID, MaxAttemptCountForWebhookFailure, 5)
	require.NoError(t, err)
	require.Equal(t, 5, count)
}

func (suite *RwApiTestSuite) TestWebhookService_CountConsecutiveFailures_NoFailures() {
	t := suite.T()
	ctx := context.Background()

	repoID, _, _ := suite.makeRandomRepo(t, suite.users.Kopatych)

	// Create a webhook
	webhook := &entities.Webhook{
		Slug:            "test-webhook-success",
		Name:            utils.PtrFromValue("Test Webhook"),
		EntityType:      entities.EntityTypes.Repository,
		EntityID:        repoID,
		PayloadURL:      "https://example.com/webhook",
		Status:          entities.WebhookStatuses.Active,
		SSLVerification: true,
		Events:          entities.WebhookEvents{"*"},
	}

	webhookID, err := suite.WebhookRepo.Create(ctx, webhook)
	require.NoError(t, err)

	// Create only successful deliveries
	for i := 0; i < 3; i++ {
		statusCode := 200
		log := &entities.WebhookLog{
			WebhookID:    webhookID,
			StatusCode:   &statusCode,
			AttemptCount: 3,
		}
		_, err := suite.WebhookLogRepo.Create(ctx, log)
		require.NoError(t, err)
	}

	// Count consecutive failures - should be 0
	count, err := suite.WebhookService.CountConsecutiveFailures(ctx, webhookID, MaxAttemptCountForWebhookFailure, 10)
	require.NoError(t, err)
	require.Equal(t, 0, count)
}

func (suite *RwApiTestSuite) TestWebhookService_CountConsecutiveFailures_IgnoresDeletedLogs() {
	t := suite.T()
	ctx := context.Background()

	repoID, _, _ := suite.makeRandomRepo(t, suite.users.Kopatych)

	// Create a webhook
	webhook := &entities.Webhook{
		Slug:            "test-webhook-deleted-count",
		Name:            utils.PtrFromValue("Test Webhook"),
		EntityType:      entities.EntityTypes.Repository,
		EntityID:        repoID,
		PayloadURL:      "https://example.com/webhook",
		Status:          entities.WebhookStatuses.Active,
		SSLVerification: true,
		Events:          entities.WebhookEvents{"*"},
	}

	webhookID, err := suite.WebhookRepo.Create(ctx, webhook)
	require.NoError(t, err)

	// Create 3 failed deliveries
	var logIDs []uint64
	for i := 0; i < 3; i++ {
		errorMsg := "Failure"
		log := &entities.WebhookLog{
			WebhookID:    webhookID,
			Error:        &errorMsg,
			AttemptCount: 3,
		}
		logID, err := suite.WebhookLogRepo.Create(ctx, log)
		require.NoError(t, err)
		logIDs = append(logIDs, logID)
	}

	// Delete one of them
	err = suite.WebhookLogRepo.Delete(ctx, logIDs[1])
	require.NoError(t, err)

	// Count should only include non-deleted logs
	count, err := suite.WebhookService.CountConsecutiveFailures(ctx, webhookID, MaxAttemptCountForWebhookFailure, 10)
	require.NoError(t, err)
	require.Equal(t, 2, count)
}
