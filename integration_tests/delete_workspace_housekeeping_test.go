package integrationtests

import (
	"context"
	"encoding/json"
	"fmt"
	"gitcore/internal/adapters/opensearch/mappings"
	"gitcore/internal/entities"
	"gitcore/internal/interfaces"
	"os"

	"github.com/stretchr/testify/require"
)

func (suite *RwApiTestSuite) TestDeleteWorkspaceHousekeeping() {
	t := suite.T()
	ctx := context.Background()

	testUser := suite.users.Kopatych

	// Verify user is in index before deletion
	hits, err := suite.OpensearchKit.GetDocumentsByType(ctx, mappings.UserMappingType)
	require.NoError(t, err)

	foundBeforeDelete := false
	for _, hit := range hits {
		var userDoc mappings.UserDocument
		err := json.Unmarshal(hit, &userDoc)
		require.NoError(t, err)
		if userDoc.ID == testUser.ID {
			foundBeforeDelete = true
			break
		}
	}
	require.True(t, foundBeforeDelete, "User should be in index before deletion")

	// Find the delete-workspace housekeeping command
	var deleteWorkspaceHK interfaces.HousekeepingMethod
	for _, hk := range suite.HouseKeepers {
		if hk.Name() == "delete-workspace" {
			deleteWorkspaceHK = hk
			break
		}
	}
	require.NotNil(t, deleteWorkspaceHK, "can't find delete-workspace housekeeper")

	// Mock stdin to simulate Enter key press
	r, w, err := os.Pipe()
	require.NoError(t, err)
	oldStdin := os.Stdin
	os.Stdin = r
	defer func() {
		os.Stdin = oldStdin
		r.Close()
		w.Close()
	}()

	_, err = w.WriteString("\n")
	require.NoError(t, err)

	// Run the housekeeping command to delete workspace
	err = deleteWorkspaceHK.Invoke(fmt.Sprintf("--id=%d", testUser.ID))
	require.NoError(t, err)

	// Wait for async index deletion
	suite.WaitForWorkflows(t, entities.WorkflowTypes.SearchIndexWorkflow)
	err = suite.OpensearchKit.RefreshIndex(ctx)
	require.NoError(t, err)

	// Verify user is deleted in DB
	userWithDeleted, err := suite.UserRepo.GetUserWithDeleted(ctx, testUser.ID)
	require.NoError(t, err)
	require.True(t, userWithDeleted.IsDeleted, "User should be marked as deleted")

	// Verify user is NOT in index anymore
	hitsAfter, err := suite.OpensearchKit.GetDocumentsByType(ctx, mappings.UserMappingType)
	require.NoError(t, err)

	foundAfterDelete := false
	for _, hit := range hitsAfter {
		var userDoc mappings.UserDocument
		err := json.Unmarshal(hit, &userDoc)
		require.NoError(t, err)
		if userDoc.ID == testUser.ID {
			foundAfterDelete = true
			break
		}
	}
	require.False(t, foundAfterDelete, "Deleted user should NOT be in index")
}
