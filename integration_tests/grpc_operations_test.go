package integrationtests

import (
	"bb.yandex-team.ru/cloud/cloud-go/genproto/privateapi/yandex/cloud/priv/operation"
	commongrpc "common/grpc/exceptions"
	"common/testutils/yarequire"
	"common/utils/idgen"
	"common/utils/rolesgenerator/iam"
	"context"
	"gitcore/internal/entities"
	"gitcore/internal/testutils"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"slices"
	"sort"
	"testing"
)

func (suite *RwApiTestSuite) createOperation(
	ID string,
	userID uint64,
	repoID uint64,
	status entities.OperationStatus,
	response proto.Message,
	pberr error,
) string {
	t := suite.T()
	op := &entities.Operation{
		ID:     ID,
		Type:   entities.OperationTypes.Stub,
		Status: status,
		UserID: userID,
		IamObject: entities.IAMObject{
			Type: entities.ObjectTypes.Repository,
			ID:   repoID,
		},
	}
	if response != nil {
		require.NoError(t, op.SetResponse(response))
	}
	if pberr != nil {
		require.NoError(t, op.SetError(pberr))
	}
	_, err := suite.OpRepo.Create(context.Background(), op)
	require.NoError(t, err)
	return op.GetID()
}

func (suite *RwApiTestSuite) TestOperationHandler_List() {
	t := suite.T()
	ctx := context.Background()
	client := pb.NewOperationServiceClient(suite.grpcClient)

	errOperationCanceled := commongrpc.NewExceptionTemplate("TestOpCanceled", "canceled", codes.Canceled)

	// create operations
	operationIDs := []string{
		suite.createOperation(
			"aengv1esjw9bhe1ks2tc",
			suite.users.Pikachu.ID,
			suite.repos.Alpha.ID,
			entities.OperationStatuses.Cancel,
			nil,
			errOperationCanceled.Build(),
		),
		suite.createOperation(
			"aengv1esjw9bhe1ks2td",
			suite.users.Pikachu.ID,
			suite.repos.Alpha.ID,
			entities.OperationStatuses.Success,
			&pb.StubOperationResponse{Message: "wow"},
			nil,
		),
		suite.createOperation(
			"aengv1esjw9bhe1ks2te",
			suite.users.Pikachu.ID,
			suite.repos.Alpha.ID,
			entities.OperationStatuses.InProgress,
			nil,
			nil,
		),
		suite.createOperation(
			"aengv1esjw9bhe1ks2tf",
			suite.users.Pikachu.ID,
			suite.repos.History.ID,
			entities.OperationStatuses.InProgress,
			nil,
			nil,
		),
	}

	require.NoError(t, suite.RepoRepo.UpdateRepositoryByID(suite.repos.History.ID).
		SetRepoVisibility(entities.Visibilities.Private).
		Commit(ctx))

	slices.Reverse(operationIDs)

	t.Run("ok", func(t *testing.T) {
		grpcCtx := testutils.AuthorizeGRPC(suite.users.Pikachu.Identity)
		response, err := client.List(grpcCtx, &pb.ListOperationsRequest{})
		require.NoError(t, err)

		sort.SliceStable(response.Operations, func(i, j int) bool {
			return response.Operations[i].CreatedAt.Seconds < response.Operations[j].CreatedAt.Seconds
		})

		//yarequire.ProtoDumpFixture(t, response)
		yarequire.ProtoCompareWithFixture(t, response,
			protocmp.IgnoreFields(&operation.Operation{},
				"id", "created_at", "modified_at"),
			protocmp.IgnoreFields(&pb.OrgIdentity{}, "id"),
			protocmp.IgnoreFields(&pb.ObjectIdentity{}, "id"))
	})

	t.Run("other user", func(t *testing.T) {
		grpcCtx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
		response, err := client.List(grpcCtx, &pb.ListOperationsRequest{})
		require.NoError(t, err)

		require.Len(t, response.Operations, 1) // Should contain at least create personal org operation
	})
}

func (suite *RwApiTestSuite) TestOperationHandler_Get() {
	t := suite.T()
	client := pb.NewOperationServiceClient(suite.grpcClient)

	operationID := suite.createOperation("aengv1esjw9bhe1ks2td", suite.users.Kopatych.ID, suite.repos.Alpha.ID, entities.OperationStatuses.InProgress, nil, nil)
	privateOpID := suite.createOperation("aengv1esjw9bhe1ks2te", suite.users.Pikachu.ID, suite.repos.History.ID, entities.OperationStatuses.InProgress, nil, nil)

	require.NoError(t, suite.RepoRepo.UpdateRepositoryByID(suite.repos.History.ID).
		SetRepoVisibility(entities.Visibilities.Private).
		Commit(context.Background()))
	suite.addRole(t, suite.users.Krosh, suite.repos.History, iam.Roles.RepositoriesContributor)

	type testcase struct {
		name         string
		user         entities.UserIdentity
		operationID  string
		expectedCode codes.Code
	}
	tests := []testcase{
		{
			name:         "ok public author",
			user:         suite.users.Kopatych.Identity,
			operationID:  operationID,
			expectedCode: codes.OK,
		},
		{
			name:         "ok public other user",
			user:         suite.users.Pikachu.Identity,
			operationID:  operationID,
			expectedCode: codes.OK,
		},
		{
			name:         "ok public anonymous",
			user:         entities.AnonymousUserIdentity,
			operationID:  operationID,
			expectedCode: codes.OK,
		},
		{
			name:         "blocked private author",
			user:         suite.users.Pikachu.Identity,
			operationID:  privateOpID,
			expectedCode: codes.PermissionDenied,
		},
		{
			name:         "permission denied private",
			user:         suite.users.Kopatych.Identity,
			operationID:  privateOpID,
			expectedCode: codes.PermissionDenied,
		},
		{
			name:         "ok private contributor",
			user:         suite.users.Krosh.Identity,
			operationID:  privateOpID,
			expectedCode: codes.OK,
		},
		{
			name:         "unknown operation",
			user:         suite.users.Kopatych.Identity,
			operationID:  "bbbbbbbbbbbbbbbbbbbb",
			expectedCode: codes.NotFound,
		},
		{
			name:         "invalid operation",
			user:         suite.users.Kopatych.Identity,
			operationID:  "some-merge-op",
			expectedCode: codes.InvalidArgument,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := client.Get(testutils.AuthorizeGRPC(test.user), &pb.GetOperationRequest{
				Id: test.operationID,
			})
			yarequire.ProtoStatusEqual(t, test.expectedCode, err)
		})
	}
}

func (suite *RwApiTestSuite) TestOperationHandler_Cancel() {
	t := suite.T()
	client := pb.NewOperationServiceClient(suite.grpcClient)

	opStatusDone := map[entities.OperationStatus]bool{
		entities.OperationStatuses.Scheduled:  false,
		entities.OperationStatuses.InProgress: false,
		entities.OperationStatuses.Success:    true,
		entities.OperationStatuses.Failed:     true,
		entities.OperationStatuses.Cancel:     true,
	}
	gen := idgen.NewGenerator(suite.cfg.YandexCloud.Env, idgen.GitCoreServiceApp)
	for opStatus, done := range opStatusDone {
		t.Run(string(opStatus), func(t *testing.T) {
			opID := suite.createOperation(gen.CreateID(),
				suite.users.Kopatych.ID, suite.repos.Alpha.ID, entities.OperationStatuses.InProgress, nil, nil)
			require.NoError(t, suite.OpRepo.Update(opID).
				SetStatus(opStatus).
				Commit(context.Background()))

			_, err := client.Cancel(testutils.AuthorizeGRPC(suite.users.Kopatych.Identity), &pb.CancelOperationRequest{
				Id: opID,
			})

			if !done {
				require.NoError(t, err)
				return
			}
			yarequire.ProtoStatusEqual(t, codes.FailedPrecondition, err)
		})
	}

	type testcase struct {
		name         string
		operationID  string
		expectedCode codes.Code
	}

	user := suite.users.Krosh
	suite.addRole(t, user, suite.repos.Alpha, iam.Roles.Admin)
	suite.addRole(t, user, suite.repos.Blame, iam.Roles.Viewer)
	require.NoError(t, suite.RepoRepo.UpdateRepositoryByID(suite.repos.Crisscross.ID).
		SetRepoVisibility(entities.Visibilities.Private).
		Commit(context.Background()))

	tests := []testcase{
		{
			name: "ok admin",
			operationID: suite.createOperation(
				gen.CreateID(),
				suite.users.Kopatych.ID,
				suite.repos.Alpha.ID,
				entities.OperationStatuses.InProgress, nil, nil,
			),
			expectedCode: codes.OK,
		},
		{
			name: "ok author",
			operationID: suite.createOperation(
				gen.CreateID(),
				suite.users.Krosh.ID,
				suite.repos.Blame.ID,
				entities.OperationStatuses.InProgress, nil, nil,
			),
			expectedCode: codes.OK,
		},
		{
			name: "blocked contributor",
			operationID: suite.createOperation(
				gen.CreateID(),
				suite.users.Kopatych.ID,
				suite.repos.Blame.ID,
				entities.OperationStatuses.InProgress, nil, nil,
			),
			expectedCode: codes.PermissionDenied,
		},
		{
			name: "blocked private author",
			operationID: suite.createOperation(
				gen.CreateID(),
				suite.users.Krosh.ID,
				suite.repos.Crisscross.ID,
				entities.OperationStatuses.InProgress, nil, nil,
			),
			expectedCode: codes.PermissionDenied,
		},
		{
			name: "blocked private",
			operationID: suite.createOperation(
				gen.CreateID(),
				suite.users.Kopatych.ID,
				suite.repos.Crisscross.ID,
				entities.OperationStatuses.InProgress, nil, nil,
			),
			expectedCode: codes.PermissionDenied,
		},
		{
			name:         "unknown operation",
			operationID:  "aaaaaaaaaaaaaaaaaaaa",
			expectedCode: codes.NotFound,
		},
		{
			name:         "invalid operation",
			operationID:  "some-op",
			expectedCode: codes.InvalidArgument,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := client.Cancel(testutils.AuthorizeGRPC(suite.users.Krosh.Identity), &pb.CancelOperationRequest{
				Id: test.operationID,
			})
			yarequire.ProtoStatusEqual(t, test.expectedCode, err)
		})
	}
}
