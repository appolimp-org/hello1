package integrationtests

import (
	"common/utils/rolesgenerator/iam"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/testutils"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

func (suite *RwApiTestSuite) TestTeamService() {
	t := suite.T()

	teamClient := pb.NewTeamServiceClient(suite.grpcClient)

	ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)

	suite.addExternalOrgRole(t, suite.users.Kopatych, suite.orgs.Yandex42, iam.Roles.OrganizationManagerAdmin)

	orgID := strconv.FormatUint(suite.orgs.Yandex42.ID, 10)

	// Test Create Team
	t.Run("Create", func(t *testing.T) {
		createReq := &pb.CreateTeamRequest{
			OrgId:       orgID,
			Name:        "test-team",
			Description: stringPtr("Test team description"),
		}

		op, err := teamClient.Create(ctx, createReq)
		require.NoError(t, err)
		require.NotNil(t, op)

		// Operations are async, just verify the operation was created successfully
		require.NotNil(t, op)
		require.NotEmpty(t, op.Id)

		var teampb pb.Team
		err = op.GetResponse().UnmarshalTo(&teampb)
		require.NoError(t, err)
		teamIdentity := teampb.GetIdentity()

		// Test GetByIdentity
		t.Run("GetByIdentity", func(t *testing.T) {
			getReq := &pb.GetTeamByIdentityRequest{
				OrgId:    orgID,
				Identity: teamIdentity,
			}

			retrievedTeam, err := teamClient.GetByIdentity(ctx, getReq)
			require.NoError(t, err)
			require.NotNil(t, retrievedTeam)
			require.Equal(t, "test-team", retrievedTeam.Name)
			require.Equal(t, "Test team description", retrievedTeam.Description)
			require.Equal(t, orgID, retrievedTeam.OrgId)
			require.Equal(t, teamIdentity.Id, retrievedTeam.Identity.Id)
		})

		// Test List Teams
		t.Run("List", func(t *testing.T) {
			listReq := &pb.ListTeamsRequest{
				OrgId:    orgID,
				PageSize: uint64Ptr(10),
			}

			listResp, err := teamClient.List(ctx, listReq)
			require.NoError(t, err)
			require.NotNil(t, listResp)
			require.NotEmpty(t, listResp.Teams)

			// Find our created team in the list
			var foundTeam *pb.Team
			for _, listedTeam := range listResp.Teams {
				if listedTeam.Identity.Id == teamIdentity.Id {
					foundTeam = listedTeam
					break
				}
			}
			require.NotNil(t, foundTeam, "Created team should be in the list")
			require.Equal(t, "test-team", foundTeam.Name)
		})

		// Test UpdateByIdentity
		t.Run("UpdateByIdentity", func(t *testing.T) {
			updateReq := &pb.UpdateTeamByIdentityRequest{
				OrgId:    orgID,
				Identity: teamIdentity,
				UpdateMask: &fieldmaskpb.FieldMask{
					Paths: []string{"name", "description"},
				},
				Name:        "updated-test-team",
				Description: "Updated test team description",
			}

			op, err := teamClient.UpdateByIdentity(ctx, updateReq)
			require.NoError(t, err)
			require.NotNil(t, op)

			// Operations are async, just verify the operation was created successfully
			require.NotNil(t, op)
			require.NotEmpty(t, op.Id)
		})

		// Test Member Management
		t.Run("MemberManagement", func(t *testing.T) {
			// Get user IDs for testing
			userID1 := suite.users.Barash.ID
			userID2 := suite.users.PinPublic.ID

			// Test AddMembers
			t.Run("AddMembers", func(t *testing.T) {
				addReq := &pb.AddMembersRequest{
					OrgId:    orgID,
					Identity: teamIdentity,
					UserIds:  []string{grpc_marshalling.IDInverse(userID1), grpc_marshalling.IDInverse(userID2)},
				}

				op, err := teamClient.AddMembers(ctx, addReq)
				require.NoError(t, err)
				require.NotNil(t, op)
				require.NotEmpty(t, op.Id)
			})

			// Test ListMembers
			t.Run("ListMembers", func(t *testing.T) {
				listMembersReq := &pb.ListMembersRequest{
					OrgId:    orgID,
					Identity: teamIdentity,
					PageSize: uint64Ptr(10),
				}

				membersResp, err := teamClient.ListMembers(ctx, listMembersReq)
				require.NoError(t, err)
				require.NotNil(t, membersResp)
				require.Contains(t, membersResp.UserIds, grpc_marshalling.IDInverse(userID1))
				require.Contains(t, membersResp.UserIds, grpc_marshalling.IDInverse(userID2))
			})

			// Test RemoveMembers
			t.Run("RemoveMembers", func(t *testing.T) {
				removeReq := &pb.RemoveMembersRequest{
					OrgId:    orgID,
					Identity: teamIdentity,
					UserIds:  []string{grpc_marshalling.IDInverse(userID1)},
				}

				op, err := teamClient.RemoveMembers(ctx, removeReq)
				require.NoError(t, err)
				require.NotNil(t, op)

				// Operations are async, just verify the operation was created successfully
				require.NotEmpty(t, op.Id)

				// Note: In a real test, you'd wait for the operation to complete before verifying
				// Verify member was removed
				listMembersReq := &pb.ListMembersRequest{
					OrgId:    orgID,
					Identity: teamIdentity,
					PageSize: uint64Ptr(10),
				}

				membersResp, err := teamClient.ListMembers(ctx, listMembersReq)
				require.NoError(t, err)
				require.NotNil(t, membersResp)
				require.NotContains(t, membersResp.UserIds, grpc_marshalling.IDInverse(userID1))
				require.Contains(t, membersResp.UserIds, grpc_marshalling.IDInverse(userID2))
			})
		})

		// Test DeleteByIdentity (should be last)
		t.Run("DeleteByIdentity", func(t *testing.T) {
			deleteReq := &pb.DeleteTeamByIdentityRequest{
				OrgId:    orgID,
				Identity: teamIdentity,
			}

			op, err := teamClient.DeleteByIdentity(ctx, deleteReq)
			require.NoError(t, err)
			require.NotNil(t, op)

			// Operations are async, just verify the operation was created successfully
			require.NotNil(t, op)
			require.NotEmpty(t, op.Id)

			// Verify team is deleted by trying to get it
			getReq := &pb.GetTeamByIdentityRequest{
				OrgId:    orgID,
				Identity: teamIdentity,
			}

			_, err = teamClient.GetByIdentity(ctx, getReq)
			require.Error(t, err, "Getting deleted team should return an error")
		})
	})
}

// Helper functions
func stringPtr(s string) *string {
	return &s
}

func uint64Ptr(u uint64) *uint64 {
	return &u
}
