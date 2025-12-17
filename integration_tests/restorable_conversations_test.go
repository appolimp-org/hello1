package integrationtests

import (
	"common/testutils/yarequire"
	"common/utils"
	"context"
	"gitcore/internal/entities"
	except "gitcore/internal/exceptions"
	"time"

	"gitcore/internal/interfaces"
	"gitcore/internal/testutils"
	"net/http"

	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"

	pb "private_api/generated/yandex/cloud/public/sourcecraft/v1"
)

func (suite *MockComradeTestSuite) TestRestorableConversations() {
	t := suite.T()

	t.Run("happy path", func(t *testing.T) {
		messages := []*pb.StoreConversationRequest_Message{
			{Role: "user", Text: "Hello, I need help with my code"},
			{Role: "assistant", Text: "Hi there! I'd be happy to help. What do you need?"},
			{Role: "user", Text: "How do I write a test in Go?"},
		}

		var capturedMessages []entities.RestorableConversationMessage
		suite.mockComradeClient.EXPECT().
			CreateConversation(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, auth interfaces.Authenticator, msgs []entities.RestorableConversationMessage, title string) (string, error) {
				capturedMessages = msgs
				return "conv-123", nil
			}).
			Times(1)

		var storeResp pb.StoreConversationResponse

		resp, err := suite.gwClient.
			As(suite.users.Habrotracker.Identity).
			SetBody(&pb.StoreConversationRequest{Messages: messages}).
			SetResult(&storeResp).
			Post("/integrations/restorable_conversations")

		yarequire.StatusCode(t, resp, err, http.StatusOK)
		err = protojson.Unmarshal(resp.Body(), &storeResp)
		require.NoError(t, err)

		require.NotEmpty(t, storeResp.ConversationId)

		grpcClient := pb.NewCodeExplanationServiceClient(suite.grpcClient)
		authCtx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)

		restoreResp, err := grpcClient.RestoreConversation(authCtx, &pb.RestoreConversationRequest{
			ConversationId: storeResp.ConversationId,
		})

		require.NoError(t, err)
		require.Equal(t, "conv-123", restoreResp.ConversationId)

		require.Len(t, capturedMessages, 3)
		for i, message := range capturedMessages {
			require.Equal(t, messages[i].Role, message.Role)
			require.Equal(t, messages[i].Text, message.Text)
		}

		// second restore fails
		_, err = grpcClient.RestoreConversation(authCtx, &pb.RestoreConversationRequest{
			ConversationId: storeResp.ConversationId,
		})

		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.NotFound, st.Code())
	})

	t.Run("code explanation - happy path", func(t *testing.T) {
		mdl, _, err := suite.Params.CodeExplanationRepository.Schedule(context.Background(), "snippet", "url", true)
		require.NoError(t, err)

		_, err = suite.Params.CodeExplanationRepository.Update(context.Background(), mdl.Hash, entities.CodeExplanationStatuses.Complete, utils.PtrFromValue("explanation"), nil)
		require.NoError(t, err)

		var capturedMessages []entities.RestorableConversationMessage
		suite.mockComradeClient.EXPECT().
			CreateConversation(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, auth interfaces.Authenticator, msgs []entities.RestorableConversationMessage, title string) (string, error) {
				capturedMessages = msgs
				return "conv-123", nil
			}).
			Times(1)

		grpcClient := pb.NewCodeExplanationServiceClient(suite.grpcClient)
		authCtx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)

		restoreResp, err := grpcClient.RestoreConversation(authCtx, &pb.RestoreConversationRequest{
			CodeExplanationHash: mdl.Hash,
		})

		require.NoError(t, err)
		require.Equal(t, "conv-123", restoreResp.ConversationId)

		require.Len(t, capturedMessages, 2)

		require.Equal(t, "user", capturedMessages[0].Role)
		require.Equal(t, "Объясни этот сниппет кода: \n```\nsnippet\n```\n", capturedMessages[0].Text)
		require.Equal(t, "assistant", capturedMessages[1].Role)
		require.Equal(t, "explanation", capturedMessages[1].Text)
	})

	t.Run("code explanation - happy path, unfinished task", func(t *testing.T) {
		_, err := suite.Params.CodeExplanationService.DropCacheByURL(context.Background(), "url")
		require.NoError(t, err)

		mdl, _, err := suite.Params.CodeExplanationRepository.Schedule(context.Background(), "snippet", "url", true)
		require.NoError(t, err)

		_, err = suite.Params.CodeExplanationRepository.Update(context.Background(), mdl.Hash, entities.CodeExplanationStatuses.Processing, utils.PtrFromValue("WIP"), nil)
		require.NoError(t, err)

		var capturedMessages []entities.RestorableConversationMessage
		suite.mockComradeClient.EXPECT().
			CreateConversation(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, auth interfaces.Authenticator, msgs []entities.RestorableConversationMessage, title string) (string, error) {
				capturedMessages = msgs
				return "conv-123", nil
			}).
			Times(1)

		grpcClient := pb.NewCodeExplanationServiceClient(suite.grpcClient)
		authCtx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)

		restoreResp, err := grpcClient.RestoreConversation(authCtx, &pb.RestoreConversationRequest{
			CodeExplanationHash: mdl.Hash,
		})

		require.NoError(t, err)
		require.Equal(t, "conv-123", restoreResp.ConversationId)

		require.Len(t, capturedMessages, 1)

		require.Equal(t, "user", capturedMessages[0].Role)
		require.Equal(t, "Объясни этот сниппет кода: \n```\nsnippet\n```\n", capturedMessages[0].Text)
	})

	t.Run("unauthorized", func(t *testing.T) {
		// wrong user
		resp, err := suite.gwClient.
			As(suite.users.Krosh.Identity).
			SetBody(&pb.StoreConversationRequest{Messages: []*pb.StoreConversationRequest_Message{}}).
			Post("/integrations/restorable_conversations")

		yarequire.StatusCode(t, resp, err, http.StatusForbidden)

		resp, err = suite.gwClient.
			AsGuest().
			SetBody(&pb.StoreConversationRequest{Messages: []*pb.StoreConversationRequest_Message{}}).
			Post("/integrations/restorable_conversations")

		require.NoError(t, err)
		yarequire.StatusCode(t, resp, err, http.StatusUnauthorized)
	})

	t.Run("restore invalid conversation", func(t *testing.T) {

		grpcClient := pb.NewCodeExplanationServiceClient(suite.grpcClient)
		authCtx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)

		_, err := grpcClient.RestoreConversation(authCtx, &pb.RestoreConversationRequest{
			ConversationId: "invalid-conversation-id", // invalid conversation id
		})

		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())

		_, err = grpcClient.RestoreConversation(authCtx, &pb.RestoreConversationRequest{
			ConversationId: uuid.New().String(), // valid but not existing conversation
		})

		require.Error(t, err)
		st, ok = status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.NotFound, st.Code())
	})

	t.Run("empty messages list", func(t *testing.T) {
		var storeResp pb.StoreConversationResponse

		resp, err := suite.gwClient.
			As(suite.users.Habrotracker.Identity).
			SetBody(&pb.StoreConversationRequest{Messages: []*pb.StoreConversationRequest_Message{}}).
			SetResult(&storeResp).
			Post("/integrations/restorable_conversations")

		yarequire.StatusCode(t, resp, err, http.StatusOK)
		err = protojson.Unmarshal(resp.Body(), &storeResp)
		require.NoError(t, err)

		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode())
		require.NotEmpty(t, storeResp.ConversationId)

		suite.mockComradeClient.EXPECT().
			CreateConversation(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, auth interfaces.Authenticator, msgs []entities.RestorableConversationMessage, title string) (string, error) {
				return "conv-321", nil
			}).
			Times(1)

		grpcClient := pb.NewCodeExplanationServiceClient(suite.grpcClient)
		authCtx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)

		restoreResp, err := grpcClient.RestoreConversation(authCtx, &pb.RestoreConversationRequest{
			ConversationId: storeResp.ConversationId,
		})

		require.NoError(t, err)
		require.Equal(t, "conv-321", restoreResp.ConversationId)
	})

	t.Run("cleanup", func(t *testing.T) {
		timeBeforeThreshold := time.Now().UTC().Add(-time.Hour * 4)
		timeAfterThreshold := time.Now().UTC().Add(-time.Hour * 2)

		expiredConversation := &entities.RestorableConversation{
			ID: uuid.New().String(),
			Messages: []entities.RestorableConversationMessage{
				{Role: "user", Text: "Hello, I need help with my code"},
				{Role: "assistant", Text: "Hi there! I'd be happy to help. What do you need?"},
			},
			CreatedAt: timeBeforeThreshold,
		}
		actualConversation := &entities.RestorableConversation{
			ID: uuid.New().String(),
			Messages: []entities.RestorableConversationMessage{
				{Role: "user", Text: "Hello"},
				{Role: "assistant", Text: "What's up?"},
			},
			CreatedAt: timeAfterThreshold,
		}
		suite.RestorableConversationsRepository.Create(context.Background(), expiredConversation)
		suite.RestorableConversationsRepository.Create(context.Background(), actualConversation)

		suite.Scheduler.ExecuteWorkflow(context.Background(), "cleanup-restorable-conversations-123", entities.WorkflowTypes.CleanupRestorableConversations, nil)
		suite.WaitForWorkflows(t, entities.WorkflowTypes.CleanupRestorableConversations)

		_, err := suite.RestorableConversationsRepository.Get(context.Background(), expiredConversation.ID)
		require.ErrorIs(t, err, except.EntityNotFound)

		_, err = suite.RestorableConversationsRepository.Get(context.Background(), actualConversation.ID)
		require.NoError(t, err)
	})
}
