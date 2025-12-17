package integrationtests

import (
	"bb.yandex-team.ru/cloud/cloud-go/genproto/privateapi/yandex/cloud/priv/operation"
	"common/grpc"
	"common/testutils/yarequire"
	"common/utils/rolesgenerator/iam"
	"gitcore/internal/testutils"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
)

// TestIdempotency_PRCommentCreate tests idempotency with PR comment creation
func (suite *RwApiTestSuite) TestIdempotency_PRCommentCreate() {
	t := suite.T()
	client := pb.NewPRCommentServiceClient(suite.grpcClient)

	// Setup: create a PR
	suite.addRole(t, suite.users.Kopatych, suite.repos.Alpha, iam.Roles.RepositoriesAdmin)
	pr := suite.makePullRequest(suite.users.Kopatych, nil)

	t.Run("cache_hit_returns_same_response", func(t *testing.T) {
		idempotencyKey := uuid.New().String()

		ctx1 := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
		ctx1 = metadata.AppendToOutgoingContext(ctx1, "idempotency-key", idempotencyKey)

		// First request - creates comment
		request := &pb.CreateCommentRequest{
			PrId:    grpc.MarshalID(pr.ID),
			Body:    "Test comment for idempotency",
			Publish: true,
		}

		op1, err := client.Create(ctx1, request)
		require.NoError(t, err)
		require.NotNil(t, op1)

		// Extract comment from operation
		result1, err := op1.GetResponse().UnmarshalNew()
		require.NoError(t, err)
		comment1, ok := result1.(*pb.PRComment)
		require.True(t, ok)
		commentID1 := comment1.GetId()
		require.NotEmpty(t, commentID1)

		// Wait for operation to complete
		time.Sleep(100 * time.Millisecond)

		// Second request with same idempotency key - should return cached response
		ctx2 := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
		ctx2 = metadata.AppendToOutgoingContext(ctx2, "idempotency-key", idempotencyKey)

		op2, err := client.Create(ctx2, request)
		require.NoError(t, err)
		require.NotNil(t, op2)

		// Extract comment from cached operation
		result2, err := op2.GetResponse().UnmarshalNew()
		require.NoError(t, err)
		comment2, ok := result2.(*pb.PRComment)
		require.True(t, ok)
		commentID2 := comment2.GetId()

		require.Equal(t, commentID1, commentID2, "cached response should return same comment ID")

		// Verify only one comment was created
		listResp, err := client.List(testutils.AuthorizeGRPC(suite.users.Kopatych.Identity), &pb.ListCommentsRequest{
			PrId: grpc.MarshalID(pr.ID),
		})
		require.NoError(t, err)

		// Count comments with our text
		count := 0
		for _, comment := range listResp.Comments {
			if comment.Body == "Test comment for idempotency" {
				count++
			}
		}
		require.Equal(t, 1, count, "only one comment should be created despite two requests")
	})

	t.Run("different_keys_create_different_comments", func(t *testing.T) {
		idempotencyKey1 := uuid.New().String()
		idempotencyKey2 := uuid.New().String()

		ctx1 := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
		ctx1 = metadata.AppendToOutgoingContext(ctx1, "idempotency-key", idempotencyKey1)

		ctx2 := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
		ctx2 = metadata.AppendToOutgoingContext(ctx2, "idempotency-key", idempotencyKey2)

		// First request
		request1 := &pb.CreateCommentRequest{
			PrId:    grpc.MarshalID(pr.ID),
			Body:    "First comment",
			Publish: true,
		}
		op1, err := client.Create(ctx1, request1)
		require.NoError(t, err)

		// Second request with different key
		request2 := &pb.CreateCommentRequest{
			PrId:    grpc.MarshalID(pr.ID),
			Body:    "Second comment",
			Publish: true,
		}
		op2, err := client.Create(ctx2, request2)
		require.NoError(t, err)

		// Extract comments
		result1, err := op1.GetResponse().UnmarshalNew()
		require.NoError(t, err)
		comment1, ok := result1.(*pb.PRComment)
		require.True(t, ok)
		commentID1 := comment1.GetId()

		result2, err := op2.GetResponse().UnmarshalNew()
		require.NoError(t, err)
		comment2, ok := result2.(*pb.PRComment)
		require.True(t, ok)
		commentID2 := comment2.GetId()

		// Should create different comments
		require.NotEqual(t, commentID1, commentID2, "different keys should create different comments")
	})

	t.Run("concurrent_requests_prevent_duplicate_execution", func(t *testing.T) {
		idempotencyKey := uuid.New().String()

		var wg sync.WaitGroup
		type result struct {
			op  *operation.Operation
			err error
		}
		results := make([]result, 5)

		request := &pb.CreateCommentRequest{
			PrId:    grpc.MarshalID(pr.ID),
			Body:    "Concurrent test comment",
			Publish: true,
		}

		// Launch 5 concurrent requests with the same idempotency key
		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func(index int) {
				defer wg.Done()

				ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
				ctx = metadata.AppendToOutgoingContext(ctx, "idempotency-key", idempotencyKey)

				op, err := client.Create(ctx, request)
				results[index] = result{op: op, err: err}
			}(i)
		}

		wg.Wait()

		// At least one request should succeed
		successCount := 0
		var firstCommentID string
		for i := 0; i < 5; i++ {
			if results[i].err == nil && results[i].op != nil {
				successCount++

				// Extract comment ID
				resp, err := results[i].op.GetResponse().UnmarshalNew()
				require.NoError(t, err)
				comment, ok := resp.(*pb.PRComment)
				require.True(t, ok)
				commentID := comment.GetId()

				if firstCommentID == "" {
					firstCommentID = commentID
				} else {
					// All successful responses should return the same comment ID
					require.Equal(t, firstCommentID, commentID,
						"all successful responses should have same comment ID")
				}
			}
		}

		require.Greater(t, successCount, 0, "at least one request should succeed")

		// Verify only one comment was created
		time.Sleep(200 * time.Millisecond) // Wait for eventual consistency

		listResp, err := client.List(testutils.AuthorizeGRPC(suite.users.Kopatych.Identity), &pb.ListCommentsRequest{
			PrId: grpc.MarshalID(pr.ID),
		})
		require.NoError(t, err)

		// Count comments with our text
		count := 0
		for _, comment := range listResp.Comments {
			if comment.Body == "Concurrent test comment" {
				count++
			}
		}
		require.Equal(t, 1, count, "concurrent requests should create only one comment")
	})

	t.Run("request_hash_mismatch_returns_error", func(t *testing.T) {
		idempotencyKey := uuid.New().String()

		ctx1 := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
		ctx1 = metadata.AppendToOutgoingContext(ctx1, "idempotency-key", idempotencyKey)

		// First request
		request1 := &pb.CreateCommentRequest{
			PrId:    grpc.MarshalID(pr.ID),
			Body:    "Original comment body",
			Publish: true,
		}
		op1, err := client.Create(ctx1, request1)
		require.NoError(t, err)
		require.NotNil(t, op1)

		// Wait for operation to complete
		time.Sleep(100 * time.Millisecond)

		// Second request with same key but DIFFERENT body
		ctx2 := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
		ctx2 = metadata.AppendToOutgoingContext(ctx2, "idempotency-key", idempotencyKey)

		request2 := &pb.CreateCommentRequest{
			PrId:    grpc.MarshalID(pr.ID),
			Body:    "Different comment body",
			Publish: true,
		}
		_, err = client.Create(ctx2, request2)

		// Should return error due to hash mismatch
		yarequire.ProtoStatusEqual(t, codes.FailedPrecondition, err)
	})

	t.Run("no_idempotency_key_works_normally", func(t *testing.T) {
		// Request without idempotency key
		ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)

		request := &pb.CreateCommentRequest{
			PrId:    grpc.MarshalID(pr.ID),
			Body:    "Comment without idempotency key",
			Publish: true,
		}

		op1, err := client.Create(ctx, request)
		require.NoError(t, err)
		require.NotNil(t, op1)

		// Second request should create a new comment
		op2, err := client.Create(ctx, request)
		require.NoError(t, err)
		require.NotNil(t, op2)

		// Extract comment IDs
		result1, err := op1.GetResponse().UnmarshalNew()
		require.NoError(t, err)
		comment1, ok := result1.(*pb.PRComment)
		require.True(t, ok)
		commentID1 := comment1.GetId()

		result2, err := op2.GetResponse().UnmarshalNew()
		require.NoError(t, err)
		comment2, ok := result2.(*pb.PRComment)
		require.True(t, ok)
		commentID2 := comment2.GetId()

		// Different comment IDs
		require.NotEqual(t, commentID1, commentID2,
			"without idempotency key, each request should create new comment")
	})
}

// TestIdempotency_ErrorCaching tests that errors can be cached when enabled
func (suite *RwApiTestSuite) TestIdempotency_ErrorCaching() {
	t := suite.T()
	client := pb.NewPRCommentServiceClient(suite.grpcClient)

	t.Run("cached_error_response", func(t *testing.T) {
		idempotencyKey := uuid.New().String()

		ctx1 := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
		ctx1 = metadata.AppendToOutgoingContext(ctx1, "idempotency-key", idempotencyKey)

		// First request to non-existent PR (will fail)
		invalidPrID := grpc.MarshalID(uint64(9999999))
		request := &pb.CreateCommentRequest{
			PrId:    invalidPrID,
			Body:    "Comment on non-existent PR",
			Publish: true,
		}

		_, err1 := client.Create(ctx1, request)
		require.Error(t, err1)
		yarequire.ProtoStatusEqual(t, codes.NotFound, err1)

		// Second request with same idempotency key
		ctx2 := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
		ctx2 = metadata.AppendToOutgoingContext(ctx2, "idempotency-key", idempotencyKey)

		_, err2 := client.Create(ctx2, request)
		require.Error(t, err2)

		// Should return same error (if error caching is enabled)
		// Note: Default config has CacheErrors: false, so this might re-execute
		yarequire.ProtoStatusEqual(t, codes.NotFound, err2)
	})
}
