package integrationtests

import (
	"common/grpc"
	"common/testutils/yarequire"
	utils2 "common/utils"
	"common/utils/rolesgenerator/iam"
	"gitcore/internal/entities"
	"gitcore/internal/services/service_connection"
	"gitcore/internal/testutils"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"testing"

	operation "bb.yandex-team.ru/cloud/cloud-go/genproto/privateapi/yandex/cloud/priv/operation"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (suite *RwApiTestSuite) TestServiceConnectionService() {
	t := suite.T()

	// Setup test context and client
	ctx := testutils.AuthorizeGRPC(testutils.UserIdentities.Kopatych)
	client := pb.NewServiceConnectionServiceClient(suite.grpcClient)

	// Test organization and repository
	org := suite.orgs.Yandex42

	suite.addExternalOrgRole(t, suite.users.Kopatych, org, iam.Roles.OrganizationManagerAdmin)
	suite.addExternalOrgRole(t, suite.users.Kopatych, org, iam.Roles.RepositoriesAdmin)

	repo := suite.RepoFixture(0, suite.orgs.Yandex42, "test-repo", testutils.BasicRepo, nil)
	repo2 := suite.RepoFixture(0, suite.orgs.Yandex42, "test-repo-2", testutils.BasicRepo, nil)

	// Test data
	serviceConnectionName := "test-service-connection"
	serviceConnectionDescription := "Test service connection for integration tests"
	folderID := "test-folder-id"
	serviceAccountID := "test-service-account-id"
	branch := "main"

	// Variable to store create operation response across tests
	var createResp *operation.Operation

	require.True(t, t.Run("GetGrantStatus", func(t *testing.T) {
		req := &pb.GetGrantStatusRequest{
			OrgId:            grpc.MarshalID(org.ID),
			BillingAccountId: "test_billing_account_id",
		}

		resp, err := client.GetGrantStatus(ctx, req)
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Equal(t, pb.GrantStatus_GRANT_STATUS_NOT_AVAILABLE, resp.GetStatus())
		require.NotEmpty(t, resp.GetDescription())
		require.Equal(t, pb.GrantUnavailableReason_UNAVAILABILITY_NON_PERSONAL_ORG, resp.GetReasonUnavailable())
	}))

	require.True(t, t.Run("CreateServiceConnection", func(t *testing.T) {
		// Test creating a service connection
		createReq := &pb.CreateServiceConnectionRequest{
			OrgId:            grpc.MarshalID(org.ID),
			FolderId:         folderID,
			Name:             serviceConnectionName,
			Description:      &serviceConnectionDescription,
			ServiceAccountId: &serviceAccountID,
			RepoId:           func() *string { s := grpc.MarshalID(repo.ID); return &s }(),
			Branch:           &branch,
			BillingAccountId: utils2.PtrFromValue("test_billing_account_id"),
			TryAddGrant:      utils2.PtrFromValue(true),
		}

		var err error
		createResp, err = client.CreateServiceConnection(ctx, createReq)
		require.NoError(t, err)
		require.NotNil(t, createResp)
		require.False(t, createResp.Done)
		require.NotEmpty(t, createResp.Id)

		// Verify operation response
		require.NotNil(t, createResp.Metadata)
		meta, err := anypb.UnmarshalNew(createResp.Metadata, proto.UnmarshalOptions{})
		require.NoError(t, err)
		yarequire.ProtoEqual(t, meta, &pb.CreateServiceConnectionMetadata{
			GrantAdded: false,
			GrantError: "grant error: organization isn't personal",
		})
		require.Equal(t, "create_service_connection", createResp.Description)
	}))

	require.True(t, t.Run("Create operation is done successfully", func(t *testing.T) {
		// Wait for the workflow to complete using suite.WaitForWorkflows
		suite.WaitForWorkflows(t, service_connection.CreateServiceConnectionWorkflowType)

		// Create operation service client to get the final operation result
		opClient := pb.NewOperationServiceClient(suite.grpcClient)

		// Get the final operation result
		finalOp, err := opClient.Get(ctx, &pb.GetOperationRequest{Id: createResp.Id})
		require.NoError(t, err)
		require.True(t, finalOp.Done)

		// Check if operation has response (successful completion)
		if finalOp.Result != nil {
			switch result := finalOp.Result.(type) {
			case *operation.Operation_Response:
				// Unmarshal the response to ServiceConnection
				var serviceConnection pb.ServiceConnection
				err = result.Response.UnmarshalTo(&serviceConnection)
				require.NoError(t, err)

				// Verify the service connection data is correct
				require.Equal(t, serviceConnectionName, serviceConnection.Name)
				require.Equal(t, serviceConnectionDescription, serviceConnection.Description)
				require.Equal(t, folderID, serviceConnection.FolderId)
				//require.Equal(t, serviceAccountID, serviceConnection.ServiceAccountId)
				require.Equal(t, grpc.MarshalID(repo.ID), serviceConnection.RepoId)
				require.Equal(t, grpc.MarshalID(org.ID), serviceConnection.OrgId)
				require.Equal(t, branch, serviceConnection.Branch)
				require.Equal(t, pb.ServiceConnection_STATUS_ENABLED, serviceConnection.Status)
				require.NotEmpty(t, serviceConnection.Id)
				require.NotNil(t, serviceConnection.CreatedAt)
				require.NotEmpty(t, serviceConnection.CreatedBy)
			case *operation.Operation_Error:
				t.Fatalf("Operation failed with error: %v", result.Error)
			default:
				t.Fatalf("Unexpected operation result type: %T", result)
			}
		}
	}))

	require.True(t, t.Run("GetServiceConnection", func(t *testing.T) {
		// Test getting a service connection
		getReq := &pb.GetServiceConnectionRequest{
			OrgId: grpc.MarshalID(org.ID),
			Name:  serviceConnectionName,
		}

		resp, err := client.GetServiceConnection(ctx, getReq)
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Equal(t, serviceConnectionName, resp.Name)
		require.Equal(t, serviceConnectionDescription, resp.Description)
		require.Equal(t, folderID, resp.FolderId)
		//require.Equal(t, serviceAccountID, resp.ServiceAccountId)
		require.Equal(t, branch, resp.Branch)
		require.Equal(t, grpc.MarshalID(repo.ID), resp.RepoId)
		require.Equal(t, grpc.MarshalID(org.ID), resp.OrgId)
		require.Equal(t, pb.ServiceConnection_STATUS_ENABLED, resp.Status)
		require.NotEmpty(t, resp.Id)
		require.NotNil(t, resp.CreatedAt)
	}))

	require.True(t, t.Run("GetServiceConnectionByCloudOrg", func(t *testing.T) {
		// Test getting a service connection
		getReq := &pb.GetServiceConnectionByCloudOrgRequest{
			OrgIamId: suite.orgs.Yandex42.Identity.ID,
			Name:     serviceConnectionName,
		}

		resp, err := client.GetServiceConnectionByCloudOrg(ctx, getReq)
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Equal(t, serviceConnectionName, resp.Name)
		require.Equal(t, serviceConnectionDescription, resp.Description)
		require.Equal(t, folderID, resp.FolderId)
		//require.Equal(t, serviceAccountID, resp.ServiceAccountId)
		require.Equal(t, branch, resp.Branch)
		require.Equal(t, grpc.MarshalID(repo.ID), resp.RepoId)
		require.Equal(t, grpc.MarshalID(org.ID), resp.OrgId)
		require.Equal(t, pb.ServiceConnection_STATUS_ENABLED, resp.Status)
		require.NotEmpty(t, resp.Id)
		require.NotNil(t, resp.CreatedAt)
	}))

	require.True(t, t.Run("ListServiceConnections", func(t *testing.T) {
		// Test listing service connections
		listReq := &pb.ListServiceConnectionsRequest{
			OrgId:    grpc.MarshalID(org.ID),
			PageSize: &[]uint64{10}[0],
		}

		resp, err := client.ListServiceConnections(ctx, listReq)
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Len(t, resp.ServiceConnections, 1)

		// Verify the listed service connection
		sc := resp.ServiceConnections[0]
		require.Equal(t, serviceConnectionName, sc.Name)
		require.Equal(t, serviceConnectionDescription, sc.Description)
		require.Equal(t, folderID, sc.FolderId)
		//require.Equal(t, serviceAccountID, sc.ServiceAccountId)
		require.Equal(t, branch, sc.Branch)
		require.Equal(t, grpc.MarshalID(repo.ID), sc.RepoId)
		require.Equal(t, grpc.MarshalID(org.ID), sc.OrgId)
		require.Equal(t, pb.ServiceConnection_STATUS_ENABLED, sc.Status)
	}))

	require.True(t, t.Run("ListServiceConnectionsByRepo", func(t *testing.T) {
		// Test listing service connections
		listReq := &pb.ListRepositoryServiceConnectionsRequest{
			OrgId:    grpc.MarshalID(org.ID),
			RepoId:   grpc.MarshalID(repo.ID),
			PageSize: &[]uint64{10}[0],
		}

		resp, err := client.ListRepositoryServiceConnections(ctx, listReq)
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Len(t, resp.ServiceConnections, 1)

		// Verify the listed service connection
		sc := resp.ServiceConnections[0]
		require.Equal(t, serviceConnectionName, sc.Name)
		require.Equal(t, serviceConnectionDescription, sc.Description)
		require.Equal(t, folderID, sc.FolderId)
		//require.Equal(t, serviceAccountID, sc.ServiceAccountId)
		require.Equal(t, branch, sc.Branch)
		require.Equal(t, grpc.MarshalID(repo.ID), sc.RepoId)
		require.Equal(t, grpc.MarshalID(org.ID), sc.OrgId)
		require.Equal(t, pb.ServiceConnection_STATUS_ENABLED, sc.Status)
	}))

	require.True(t, t.Run("ListServiceConnections_WithPagination", func(t *testing.T) {
		// Test pagination with page size 1
		listReq := &pb.ListServiceConnectionsRequest{
			OrgId:    grpc.MarshalID(org.ID),
			PageSize: &[]uint64{1}[0],
		}

		resp, err := client.ListServiceConnections(ctx, listReq)
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Len(t, resp.ServiceConnections, 1)

		// Since we only have one service connection, next page token should be empty
		require.Empty(t, resp.NextPageToken)
	}))

	// Variable to store update operation response across tests
	var updateResp *operation.Operation

	require.True(t, t.Run("UpdateServiceConnection", func(t *testing.T) {
		// Test updating a service connection
		updateReq := &pb.UpdateServiceConnectionRequest{
			OrgId:            grpc.MarshalID(org.ID),
			Name:             serviceConnectionName,
			ServiceAccountId: &serviceAccountID,
			RepoId:           func() *string { s := grpc.MarshalID(repo2.ID); return &s }(),
			Branch:           &branch,
		}

		var err error
		updateResp, err = client.UpdateServiceConnection(ctx, updateReq)
		require.NoError(t, err)
		require.NotNil(t, updateResp)
		require.False(t, updateResp.Done)
		require.NotEmpty(t, updateResp.Id)

		// Verify operation response
		require.NotNil(t, updateResp.Metadata)
		require.Equal(t, "update_service_connection", updateResp.Description)

		suite.WaitForWorkflows(t, service_connection.UpdateServiceConnectionWorkflowType)

		// Create operation service client to get the update operation result
		opClient := pb.NewOperationServiceClient(suite.grpcClient)

		// Get the update operation result
		updateOp, err := opClient.Get(ctx, &pb.GetOperationRequest{Id: updateResp.Id})
		require.NoError(t, err)
		require.True(t, updateOp.Done)
	}))

	// Variable to store delete operation response across tests
	var deleteResp *operation.Operation

	require.True(t, t.Run("DeleteServiceConnection", func(t *testing.T) {
		// Test deleting a service connection
		deleteReq := &pb.DeleteServiceConnectionRequest{
			OrgId: grpc.MarshalID(org.ID),
			Name:  serviceConnectionName,
		}

		var err error
		deleteResp, err = client.DeleteServiceConnection(ctx, deleteReq)
		require.NoError(t, err)
		require.NotNil(t, deleteResp)
		require.False(t, deleteResp.Done)
		require.NotEmpty(t, deleteResp.Id)

		// Verify operation response
		require.NotNil(t, deleteResp.Metadata)
		require.Equal(t, "delete_service_connection", deleteResp.Description)
	}))

	require.True(t, t.Run("Delete operation is done successfully", func(t *testing.T) {
		// Wait for the workflow to complete using suite.WaitForWorkflows
		suite.WaitForWorkflows(t, service_connection.DeleteServiceConnectionWorkflowType)

		// Create operation service client to get the final operation result
		opClient := pb.NewOperationServiceClient(suite.grpcClient)

		// Get the final operation result
		finalOp, err := opClient.Get(ctx, &pb.GetOperationRequest{Id: deleteResp.Id})
		require.NoError(t, err)
		require.True(t, finalOp.Done)

		// Check if operation has response (successful completion)
		if finalOp.Result != nil {
			switch result := finalOp.Result.(type) {
			case *operation.Operation_Response:
				// Unmarshal the response to DeleteServiceConnectionResponse
				var deleteResponse pb.DeleteServiceConnectionResponse
				err = result.Response.UnmarshalTo(&deleteResponse)
				require.NoError(t, err)
			case *operation.Operation_Error:
				t.Fatalf("Operation failed with error: %v", result.Error)
			default:
				t.Fatalf("Unexpected operation result type: %T", result)
			}
		}

		// Verify the service connection is deleted by trying to get it
		getReq := &pb.GetServiceConnectionRequest{
			OrgId: grpc.MarshalID(org.ID),
			Name:  serviceConnectionName,
		}

		_, err = client.GetServiceConnection(ctx, getReq)
		require.Error(t, err)
		require.Equal(t, codes.NotFound, status.Code(err))
	}))
}

func (suite *RwApiTestSuite) TestServiceConnectionService_TestMiscMethods() {
	t := suite.T()

	ctx := testutils.AuthorizeGRPC(testutils.UserIdentities.Kopatych)
	client := pb.NewServiceConnectionServiceClient(suite.grpcClient)

	suite.addExternalOrgRole(t, suite.users.Kopatych, suite.orgs.Yandex42, iam.Roles.OrganizationManagerAdmin)
	suite.addExternalOrgRole(t, suite.users.Kopatych, suite.orgs.Yandex42, iam.Roles.RepositoriesAdmin)

	_, err := client.ListFolderServiceAccounts(ctx, &pb.ListFolderServiceAccountsRequest{
		OrgId:    entities.Uint64EntityID(suite.orgs.Yandex42.ID).String(),
		FolderId: "test-folder-id",
	})
	require.NoError(t, err)

	_, err = client.ListOrganizationFolders(ctx, &pb.ListOrganizationFoldersRequest{
		OrgId: entities.Uint64EntityID(suite.orgs.Yandex42.ID).String(),
	})
	require.NoError(t, err)
}
