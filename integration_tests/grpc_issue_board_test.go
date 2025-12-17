package integrationtests

import (
	"common/functools"
	"common/utils"
	"context"
	"fmt"
	"gitcore/internal/entities"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/testutils"
	pagination_pb "private_api/generated/yandex/cloud/pagination"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (suite *RwApiTestSuite) TestGrpcListBoardIssues() {
	t := suite.T()
	client := pb.NewIssueServiceClient(suite.grpcClient)
	repoID, _, _ := suite.makeRandomRepo(t, suite.users.Admin)

	// create issues
	openIssue1 := suite.makeIssue(suite.users.Admin, &makeIssueOptions{
		RepoID: repoID,
		Status: entities.IssueStatuses.Open,
		Title:  "Open issue 1",
	})
	openIssue2 := suite.makeIssue(suite.users.Admin, &makeIssueOptions{
		RepoID: repoID,
		Status: entities.IssueStatuses.Open,
		Title:  "Open issue 2",
	})
	progressIssue := suite.makeIssue(suite.users.Kopatych, &makeIssueOptions{
		RepoID: repoID,
		Status: entities.IssueStatuses.InProgress,
		Title:  "In progress issue",
	})
	declinedIssue := suite.makeIssue(suite.users.Admin, &makeIssueOptions{
		RepoID: repoID,
		Status: entities.IssueStatuses.Declined,
		Title:  "Declined issue",
	})
	duplicateIssue := suite.makeIssue(suite.users.Kopatych, &makeIssueOptions{
		RepoID: repoID,
		Status: entities.IssueStatuses.Duplicate,
		Title:  "Duplicate issue",
	})
	privateIssue := suite.makeIssue(suite.users.Admin, &makeIssueOptions{
		RepoID:     repoID,
		Status:     entities.IssueStatuses.Open,
		Title:      "Private issue",
		Visibility: entities.IssueVisibilities.Private,
	})

	tests := []struct {
		name           string
		user           *entities.User
		req            *pb.ListBoardIssuesRequest
		expectedIDs    map[pb.IssueStatus_StatusType][]uint64
		expectedStatus codes.Code
	}{
		{
			name: "basic board structure",
			user: suite.users.Admin,
			req: &pb.ListBoardIssuesRequest{
				RepoId: grpc_marshalling.IDInverse(repoID),
				Columns: []*pb.BoardColumnRequest{
					{StatusType: pb.IssueStatus_STATUS_TYPE_INITIAL},
					{StatusType: pb.IssueStatus_STATUS_TYPE_IN_PROGRESS},
					{StatusType: pb.IssueStatus_STATUS_TYPE_CANCELED},
				},
			},
			expectedIDs: map[pb.IssueStatus_StatusType][]uint64{
				pb.IssueStatus_STATUS_TYPE_INITIAL:     {openIssue1.ID, openIssue2.ID, privateIssue.ID},
				pb.IssueStatus_STATUS_TYPE_IN_PROGRESS: {progressIssue.ID},
				pb.IssueStatus_STATUS_TYPE_CANCELED:    {declinedIssue.ID, duplicateIssue.ID},
			},
		},
		{
			name: "partial columns",
			user: suite.users.Admin,
			req: &pb.ListBoardIssuesRequest{
				RepoId: grpc_marshalling.IDInverse(repoID),
				Columns: []*pb.BoardColumnRequest{
					{StatusType: pb.IssueStatus_STATUS_TYPE_IN_PROGRESS},
					{StatusType: pb.IssueStatus_STATUS_TYPE_CANCELED},
				},
			},
			expectedIDs: map[pb.IssueStatus_StatusType][]uint64{
				pb.IssueStatus_STATUS_TYPE_IN_PROGRESS: {progressIssue.ID},
				pb.IssueStatus_STATUS_TYPE_CANCELED:    {declinedIssue.ID, duplicateIssue.ID},
			},
		},
		{
			name: "private issue visibility",
			user: suite.users.Kopatych,
			req: &pb.ListBoardIssuesRequest{
				RepoId: grpc_marshalling.IDInverse(repoID),
				Columns: []*pb.BoardColumnRequest{
					{StatusType: pb.IssueStatus_STATUS_TYPE_INITIAL},
				},
			},
			expectedIDs: map[pb.IssueStatus_StatusType][]uint64{
				pb.IssueStatus_STATUS_TYPE_INITIAL: {openIssue1.ID, openIssue2.ID},
			},
		},
	}

	for _, tc := range tests {
		suite.T().Run(tc.name, func(t *testing.T) {
			ctx := testutils.AuthorizeGRPC(tc.user.Identity)
			resp, err := client.ListBoardIssues(ctx, tc.req)

			if tc.expectedStatus != 0 {
				st, ok := status.FromError(err)
				require.True(t, ok)
				require.Equal(t, tc.expectedStatus, st.Code())
				return
			}
			require.NoError(t, err)

			// check columns
			require.Len(t, resp.Columns, len(tc.expectedIDs))
			for _, col := range resp.Columns {
				expected := tc.expectedIDs[col.StatusType]
				actual := getIssueIDs(col.Issues)
				require.ElementsMatch(t, expected, actual)
			}
		})
	}
}

func (suite *RwApiTestSuite) TestGrpcListBoardIssuesWithPagination() {
	t := suite.T()
	client := pb.NewIssueServiceClient(suite.grpcClient)
	repoID, _, _ := suite.makeRandomRepo(t, suite.users.Admin)

	// create issues in 2 statuses
	var openIssues []*entities.Issue
	var progressIssues []*entities.Issue
	for i := 1; i <= 5; i++ {
		openIssues = append(openIssues, suite.makeIssue(suite.users.Admin, &makeIssueOptions{
			RepoID: repoID,
			Status: entities.IssueStatuses.Open,
			Title:  fmt.Sprintf("Open issue %d", i),
		}))

		progressIssues = append(progressIssues, suite.makeIssue(suite.users.Kopatych, &makeIssueOptions{
			RepoID: repoID,
			Status: entities.IssueStatuses.InProgress,
			Title:  fmt.Sprintf("Progress issue %d", i),
		}))
	}

	ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)

	suite.T().Run("multi-column pagination", func(t *testing.T) {
		resp, err := client.ListBoardIssues(ctx, &pb.ListBoardIssuesRequest{
			RepoId: grpc_marshalling.IDInverse(repoID),
			Columns: []*pb.BoardColumnRequest{
				{
					StatusType: pb.IssueStatus_STATUS_TYPE_INITIAL,
					PageSize:   utils.PtrFromValue(uint64(2)),
				},
				{
					StatusType: pb.IssueStatus_STATUS_TYPE_IN_PROGRESS,
					PageSize:   utils.PtrFromValue(uint64(3)),
				},
			},
		})
		require.NoError(t, err)
		require.Len(t, resp.Columns, 2)

		// open
		openCol := findColumn(resp.Columns, pb.IssueStatus_STATUS_TYPE_INITIAL)
		require.Len(t, openCol.Issues, 2)
		require.ElementsMatch(t, getIDs(openIssues[:2]), getIssueIDs(openCol.Issues))
		require.NotEmpty(t, openCol.NextPageToken)

		// in progress
		progressCol := findColumn(resp.Columns, pb.IssueStatus_STATUS_TYPE_IN_PROGRESS)
		require.Len(t, progressCol.Issues, 3)
		require.ElementsMatch(t, getIDs(progressIssues[:3]), getIssueIDs(progressCol.Issues))
		require.NotEmpty(t, progressCol.NextPageToken)

		// second page
		resp2, err := client.ListBoardIssues(ctx, &pb.ListBoardIssuesRequest{
			RepoId: grpc_marshalling.IDInverse(repoID),
			Columns: []*pb.BoardColumnRequest{
				{
					StatusType: pb.IssueStatus_STATUS_TYPE_INITIAL,
					PageToken:  &openCol.NextPageToken,
				},
				{
					StatusType: pb.IssueStatus_STATUS_TYPE_IN_PROGRESS,
					PageToken:  &progressCol.NextPageToken,
				},
			},
		})
		require.NoError(t, err)

		// open
		openCol2 := findColumn(resp2.Columns, pb.IssueStatus_STATUS_TYPE_INITIAL)
		require.ElementsMatch(t, getIDs(openIssues[2:]), getIssueIDs(openCol2.Issues))
		require.Empty(t, openCol2.NextPageToken)

		// in progress
		progressCol2 := findColumn(resp2.Columns, pb.IssueStatus_STATUS_TYPE_IN_PROGRESS)
		require.ElementsMatch(t, getIDs(progressIssues[3:]), getIssueIDs(progressCol2.Issues))
		require.Empty(t, progressCol2.NextPageToken)
	})
}

func (suite *RwApiTestSuite) TestGrpcListBoardIssuesWithSort() {
	t := suite.T()
	client := pb.NewIssueServiceClient(suite.grpcClient)
	repoID, _, _ := suite.makeRandomRepo(t, suite.users.Admin)

	issue1 := suite.makeIssue(suite.users.Admin, &makeIssueOptions{
		RepoID:     repoID,
		Status:     entities.IssueStatuses.Open,
		Title:      "Issue 1",
		AssigneeID: &suite.users.Kopatych.ID,
	})
	issue2 := suite.makeIssue(suite.users.Admin, &makeIssueOptions{
		RepoID:     repoID,
		Status:     entities.IssueStatuses.Open,
		Title:      "Issue 2",
		AssigneeID: &suite.users.Krosh.ID,
	})
	issue3 := suite.makeIssue(suite.users.Admin, &makeIssueOptions{
		RepoID:     repoID,
		Status:     entities.IssueStatuses.Open,
		Title:      "Issue 3",
		AssigneeID: &suite.users.Kopatych.ID,
	})

	ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)

	tests := []struct {
		name     string
		sortBy   []*pagination_pb.SortOption
		expected []uint64
	}{
		{
			name: "sort by assignee_id asc",
			sortBy: []*pagination_pb.SortOption{
				{
					Column:    "assignee_id",
					Direction: pagination_pb.SortOption_ASC,
				},
			},
			expected: []uint64{issue1.ID, issue3.ID, issue2.ID}, // Kopatych (1, 3), Krosh (2)
		},
		{
			name: "sort by assignee_id desc",
			sortBy: []*pagination_pb.SortOption{
				{
					Column:    "assignee_id",
					Direction: pagination_pb.SortOption_DESC,
				},
			},
			expected: []uint64{issue2.ID, issue1.ID, issue3.ID}, // Krosh (2), Kopatych (1, 3)
		},
	}

	for _, tc := range tests {
		suite.T().Run(tc.name, func(t *testing.T) {
			resp, err := client.ListBoardIssues(ctx, &pb.ListBoardIssuesRequest{
				RepoId: grpc_marshalling.IDInverse(repoID),
				Columns: []*pb.BoardColumnRequest{
					{StatusType: pb.IssueStatus_STATUS_TYPE_INITIAL},
				},
				SortBy: tc.sortBy,
			})
			require.NoError(t, err)

			actualIDs := getIssueIDs(resp.Columns[0].Issues)
			require.Equal(t, tc.expected, actualIDs)
		})
	}
}

func (suite *RwApiTestSuite) TestGrpcListBoardIssuesWithFilter() {
	t := suite.T()
	client := pb.NewIssueServiceClient(suite.grpcClient)
	repoID, _, _ := suite.makeRandomRepo(t, suite.users.Admin)

	adminIssue := suite.makeIssue(suite.users.Admin, &makeIssueOptions{RepoID: repoID, Status: entities.IssueStatuses.Open})
	suite.makeIssue(suite.users.Kopatych, &makeIssueOptions{RepoID: repoID, Status: entities.IssueStatuses.Open})

	ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)

	tests := []struct {
		name     string
		filter   *pagination_pb.Filter
		expected []uint64
	}{
		{
			name: "filter by author",
			filter: &pagination_pb.Filter{
				Filter: &pagination_pb.Filter_Predicate{
					Predicate: &pagination_pb.Predicate{
						Field:    "author_id",
						Operator: pagination_pb.Operator_OPERATOR_EQ,
						Operand: &pagination_pb.Predicate_StringValue{
							StringValue: grpc_marshalling.IDInverse(suite.users.Admin.ID),
						},
					},
				},
			},
			expected: []uint64{adminIssue.ID},
		},
	}

	for _, tc := range tests {
		suite.T().Run(tc.name, func(t *testing.T) {
			resp, err := client.ListBoardIssues(ctx, &pb.ListBoardIssuesRequest{
				RepoId: grpc_marshalling.IDInverse(repoID),
				Columns: []*pb.BoardColumnRequest{
					{StatusType: pb.IssueStatus_STATUS_TYPE_INITIAL},
				},
				Filter: tc.filter,
			})
			require.NoError(t, err)

			actualIDs := getIssueIDs(resp.Columns[0].Issues)
			require.ElementsMatch(t, tc.expected, actualIDs)
		})
	}
}

func (suite *RwApiTestSuite) TestGrpcListBoardIssuesWithPaginationAndQuery() {
	t := suite.T()
	suite.RestoreOpensearch()
	client := pb.NewIssueServiceClient(suite.grpcClient)
	repoID, _, _ := suite.makeRandomRepo(t, suite.users.Admin)

	// create issues in 2 statuses
	var openIssues []*entities.Issue
	var progressIssues []*entities.Issue
	for i := 1; i <= 5; i++ {
		openIssues = append(openIssues, suite.makeIssue(suite.users.Admin, &makeIssueOptions{
			RepoID: repoID,
			Status: entities.IssueStatuses.Open,
			Title:  fmt.Sprintf("Open issue %d", i),
		}))
		suite.makeIssue(suite.users.Admin, &makeIssueOptions{
			RepoID: repoID,
			Status: entities.IssueStatuses.Open,
			Title:  fmt.Sprintf("Open %d", i),
		})

		progressIssues = append(progressIssues, suite.makeIssue(suite.users.Kopatych, &makeIssueOptions{
			RepoID: repoID,
			Status: entities.IssueStatuses.InProgress,
			Title:  fmt.Sprintf("Progress issue %d", i),
		}))
		suite.makeIssue(suite.users.Kopatych, &makeIssueOptions{
			RepoID: repoID,
			Status: entities.IssueStatuses.InProgress,
			Title:  fmt.Sprintf("Progress %d", i),
		})
	}

	suite.WaitForWorkflows(t, entities.WorkflowTypes.SearchIndexWorkflow)
	err := suite.OpensearchKit.RefreshIndex(context.Background())
	require.NoError(t, err)

	ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)

	suite.T().Run("multi-column pagination", func(t *testing.T) {
		resp, err := client.ListBoardIssues(ctx, &pb.ListBoardIssuesRequest{
			RepoId: grpc_marshalling.IDInverse(repoID),
			Query:  utils.PtrFromValue("issue"),
			Columns: []*pb.BoardColumnRequest{
				{
					StatusType: pb.IssueStatus_STATUS_TYPE_INITIAL,
					PageSize:   utils.PtrFromValue(uint64(2)),
				},
				{
					StatusType: pb.IssueStatus_STATUS_TYPE_IN_PROGRESS,
					PageSize:   utils.PtrFromValue(uint64(3)),
				},
			},
		})
		require.NoError(t, err)
		require.Len(t, resp.Columns, 2)

		// open
		openCol := findColumn(resp.Columns, pb.IssueStatus_STATUS_TYPE_INITIAL)
		require.Len(t, openCol.Issues, 2)
		require.ElementsMatch(t, getIDs(openIssues[:2]), getIssueIDs(openCol.Issues))
		require.NotEmpty(t, openCol.NextPageToken)

		// in progress
		progressCol := findColumn(resp.Columns, pb.IssueStatus_STATUS_TYPE_IN_PROGRESS)
		require.Len(t, progressCol.Issues, 3)
		require.ElementsMatch(t, getIDs(progressIssues[:3]), getIssueIDs(progressCol.Issues))
		require.NotEmpty(t, progressCol.NextPageToken)

		// second page
		resp2, err := client.ListBoardIssues(ctx, &pb.ListBoardIssuesRequest{
			RepoId: grpc_marshalling.IDInverse(repoID),
			Query:  utils.PtrFromValue("issue"),
			Columns: []*pb.BoardColumnRequest{
				{
					StatusType: pb.IssueStatus_STATUS_TYPE_INITIAL,
					PageToken:  &openCol.NextPageToken,
				},
				{
					StatusType: pb.IssueStatus_STATUS_TYPE_IN_PROGRESS,
					PageToken:  &progressCol.NextPageToken,
				},
			},
		})
		require.NoError(t, err)

		// open
		openCol2 := findColumn(resp2.Columns, pb.IssueStatus_STATUS_TYPE_INITIAL)
		require.ElementsMatch(t, getIDs(openIssues[2:]), getIssueIDs(openCol2.Issues))
		require.Empty(t, openCol2.NextPageToken)

		// in progress
		progressCol2 := findColumn(resp2.Columns, pb.IssueStatus_STATUS_TYPE_IN_PROGRESS)
		require.ElementsMatch(t, getIDs(progressIssues[3:]), getIssueIDs(progressCol2.Issues))
		require.Empty(t, progressCol2.NextPageToken)
	})
}

func (suite *RwApiTestSuite) TestGrpcListBoardIssuesWithSortAndQuery() {
	t := suite.T()
	suite.RestoreOpensearch()
	client := pb.NewIssueServiceClient(suite.grpcClient)
	repoID, _, _ := suite.makeRandomRepo(t, suite.users.Admin)

	issue1 := suite.makeIssue(suite.users.Admin, &makeIssueOptions{
		RepoID:     repoID,
		Status:     entities.IssueStatuses.Open,
		Title:      "Issue 1",
		AssigneeID: &suite.users.Kopatych.ID,
	})
	issue2 := suite.makeIssue(suite.users.Admin, &makeIssueOptions{
		RepoID:     repoID,
		Status:     entities.IssueStatuses.Open,
		Title:      "Issue 2",
		AssigneeID: &suite.users.Krosh.ID,
	})
	issue3 := suite.makeIssue(suite.users.Admin, &makeIssueOptions{
		RepoID:     repoID,
		Status:     entities.IssueStatuses.Open,
		Title:      "Issue 3",
		AssigneeID: &suite.users.Kopatych.ID,
	})
	suite.makeIssue(suite.users.Admin, &makeIssueOptions{
		RepoID:     repoID,
		Status:     entities.IssueStatuses.Open,
		Title:      "1",
		AssigneeID: &suite.users.Kopatych.ID,
	})
	suite.makeIssue(suite.users.Admin, &makeIssueOptions{
		RepoID:     repoID,
		Status:     entities.IssueStatuses.Open,
		Title:      "2",
		AssigneeID: &suite.users.Krosh.ID,
	})
	suite.makeIssue(suite.users.Admin, &makeIssueOptions{
		RepoID:     repoID,
		Status:     entities.IssueStatuses.Open,
		Title:      "3",
		AssigneeID: &suite.users.Kopatych.ID,
	})

	suite.WaitForWorkflows(t, entities.WorkflowTypes.SearchIndexWorkflow)
	err := suite.OpensearchKit.RefreshIndex(context.Background())
	require.NoError(t, err)

	ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)

	tests := []struct {
		name     string
		sortBy   []*pagination_pb.SortOption
		expected []uint64
	}{
		{
			name: "sort by assignee_id asc",
			sortBy: []*pagination_pb.SortOption{
				{
					Column:    "assignee_id",
					Direction: pagination_pb.SortOption_ASC,
				},
			},
			expected: []uint64{issue1.ID, issue3.ID, issue2.ID}, // Kopatych (1, 3), Krosh (2)
		},
		{
			name: "sort by assignee_id desc",
			sortBy: []*pagination_pb.SortOption{
				{
					Column:    "assignee_id",
					Direction: pagination_pb.SortOption_DESC,
				},
			},
			expected: []uint64{issue2.ID, issue1.ID, issue3.ID}, // Krosh (2), Kopatych (1, 3)
		},
	}

	for _, tc := range tests {
		suite.T().Run(tc.name, func(t *testing.T) {
			resp, err := client.ListBoardIssues(ctx, &pb.ListBoardIssuesRequest{
				Query:  utils.PtrFromValue("issue"),
				RepoId: grpc_marshalling.IDInverse(repoID),
				Columns: []*pb.BoardColumnRequest{
					{StatusType: pb.IssueStatus_STATUS_TYPE_INITIAL},
				},
				SortBy: tc.sortBy,
			})
			require.NoError(t, err)

			actualIDs := getIssueIDs(resp.Columns[0].Issues)
			require.Equal(t, tc.expected, actualIDs)
		})
	}
}

func (suite *RwApiTestSuite) TestGrpcListBoardIssuesWithFilterAndQuery() {
	t := suite.T()
	suite.RestoreOpensearch()
	client := pb.NewIssueServiceClient(suite.grpcClient)
	repoID, _, _ := suite.makeRandomRepo(t, suite.users.Admin)

	adminIssue := suite.makeIssue(suite.users.Admin, &makeIssueOptions{
		RepoID: repoID,
		Status: entities.IssueStatuses.Open,
		Title:  "Admin issue",
	})
	suite.makeIssue(suite.users.Admin, &makeIssueOptions{
		RepoID: repoID,
		Status: entities.IssueStatuses.Open,
		Title:  "Admin",
	})
	suite.makeIssue(suite.users.Kopatych, &makeIssueOptions{
		RepoID: repoID,
		Status: entities.IssueStatuses.Open,
	})

	ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)

	suite.WaitForWorkflows(t, entities.WorkflowTypes.SearchIndexWorkflow)
	err := suite.OpensearchKit.RefreshIndex(context.Background())
	require.NoError(t, err)

	tests := []struct {
		name     string
		filter   *pagination_pb.Filter
		expected []uint64
	}{
		{
			name: "filter by author",
			filter: &pagination_pb.Filter{
				Filter: &pagination_pb.Filter_Predicate{
					Predicate: &pagination_pb.Predicate{
						Field:    "author_id",
						Operator: pagination_pb.Operator_OPERATOR_EQ,
						Operand: &pagination_pb.Predicate_StringValue{
							StringValue: grpc_marshalling.IDInverse(suite.users.Admin.ID),
						},
					},
				},
			},
			expected: []uint64{adminIssue.ID},
		},
	}

	for _, tc := range tests {
		suite.T().Run(tc.name, func(t *testing.T) {
			resp, err := client.ListBoardIssues(ctx, &pb.ListBoardIssuesRequest{
				RepoId: grpc_marshalling.IDInverse(repoID),
				Columns: []*pb.BoardColumnRequest{
					{StatusType: pb.IssueStatus_STATUS_TYPE_INITIAL},
				},
				Filter: tc.filter,
				Query:  utils.PtrFromValue("Issue"),
			})
			require.NoError(t, err)

			actualIDs := getIssueIDs(resp.Columns[0].Issues)
			require.ElementsMatch(t, tc.expected, actualIDs)
		})
	}
}

func (suite *RwApiTestSuite) TestGrpcListBoardWithQuery() {
	t := suite.T()
	suite.RestoreOpensearch()
	client := pb.NewIssueServiceClient(suite.grpcClient)
	repoID, _, _ := suite.makeRandomRepo(t, suite.users.Admin)

	// create issues
	openIssue1 := suite.makeIssue(suite.users.Admin, &makeIssueOptions{
		RepoID: repoID,
		Status: entities.IssueStatuses.Open,
		Title:  "Open issue 1",
	})
	suite.makeIssue(suite.users.Admin, &makeIssueOptions{
		RepoID: repoID,
		Status: entities.IssueStatuses.Open,
		Title:  "Open 1",
	})
	openIssue2 := suite.makeIssue(suite.users.Admin, &makeIssueOptions{
		RepoID: repoID,
		Status: entities.IssueStatuses.Open,
		Title:  "Open issue 2",
	})
	suite.makeIssue(suite.users.Admin, &makeIssueOptions{
		RepoID: repoID,
		Status: entities.IssueStatuses.Open,
		Title:  "Open 2",
	})
	progressIssue := suite.makeIssue(suite.users.Kopatych, &makeIssueOptions{
		RepoID: repoID,
		Status: entities.IssueStatuses.InProgress,
		Title:  "In progress issue",
	})
	suite.makeIssue(suite.users.Kopatych, &makeIssueOptions{
		RepoID: repoID,
		Status: entities.IssueStatuses.InProgress,
		Title:  "In progress",
	})
	declinedIssue := suite.makeIssue(suite.users.Admin, &makeIssueOptions{
		RepoID: repoID,
		Status: entities.IssueStatuses.Declined,
		Title:  "Declined issue",
	})
	suite.makeIssue(suite.users.Admin, &makeIssueOptions{
		RepoID: repoID,
		Status: entities.IssueStatuses.Declined,
		Title:  "Declined",
	})
	duplicateIssue := suite.makeIssue(suite.users.Kopatych, &makeIssueOptions{
		RepoID: repoID,
		Status: entities.IssueStatuses.Duplicate,
		Title:  "Duplicate issue",
	})
	suite.makeIssue(suite.users.Kopatych, &makeIssueOptions{
		RepoID: repoID,
		Status: entities.IssueStatuses.Duplicate,
		Title:  "Duplicate",
	})
	privateIssue := suite.makeIssue(suite.users.Admin, &makeIssueOptions{
		RepoID:     repoID,
		Status:     entities.IssueStatuses.Open,
		Title:      "Private issue",
		Visibility: entities.IssueVisibilities.Private,
	})
	suite.makeIssue(suite.users.Admin, &makeIssueOptions{
		RepoID:     repoID,
		Status:     entities.IssueStatuses.Open,
		Title:      "Private",
		Visibility: entities.IssueVisibilities.Private,
	})

	suite.WaitForWorkflows(t, entities.WorkflowTypes.SearchIndexWorkflow)
	err := suite.OpensearchKit.RefreshIndex(context.Background())
	require.NoError(t, err)

	tests := []struct {
		name           string
		user           *entities.User
		req            *pb.ListBoardIssuesRequest
		expectedIDs    map[pb.IssueStatus_StatusType][]uint64
		expectedStatus codes.Code
	}{
		{
			name: "basic board structure",
			user: suite.users.Admin,
			req: &pb.ListBoardIssuesRequest{
				RepoId: grpc_marshalling.IDInverse(repoID),
				Columns: []*pb.BoardColumnRequest{
					{StatusType: pb.IssueStatus_STATUS_TYPE_INITIAL},
					{StatusType: pb.IssueStatus_STATUS_TYPE_IN_PROGRESS},
					{StatusType: pb.IssueStatus_STATUS_TYPE_CANCELED},
				},
				Query: utils.PtrFromValue("issue"),
			},
			expectedIDs: map[pb.IssueStatus_StatusType][]uint64{
				pb.IssueStatus_STATUS_TYPE_INITIAL:     {openIssue1.ID, openIssue2.ID, privateIssue.ID},
				pb.IssueStatus_STATUS_TYPE_IN_PROGRESS: {progressIssue.ID},
				pb.IssueStatus_STATUS_TYPE_CANCELED:    {declinedIssue.ID, duplicateIssue.ID},
			},
		},
		{
			name: "partial columns",
			user: suite.users.Admin,
			req: &pb.ListBoardIssuesRequest{
				RepoId: grpc_marshalling.IDInverse(repoID),
				Columns: []*pb.BoardColumnRequest{
					{StatusType: pb.IssueStatus_STATUS_TYPE_IN_PROGRESS},
					{StatusType: pb.IssueStatus_STATUS_TYPE_CANCELED},
				},
				Query: utils.PtrFromValue("issue"),
			},
			expectedIDs: map[pb.IssueStatus_StatusType][]uint64{
				pb.IssueStatus_STATUS_TYPE_IN_PROGRESS: {progressIssue.ID},
				pb.IssueStatus_STATUS_TYPE_CANCELED:    {declinedIssue.ID, duplicateIssue.ID},
			},
		},
		{
			name: "private issue visibility",
			user: suite.users.Kopatych,
			req: &pb.ListBoardIssuesRequest{
				RepoId: grpc_marshalling.IDInverse(repoID),
				Columns: []*pb.BoardColumnRequest{
					{StatusType: pb.IssueStatus_STATUS_TYPE_INITIAL},
				},
				Query: utils.PtrFromValue("issue"),
			},
			expectedIDs: map[pb.IssueStatus_StatusType][]uint64{
				pb.IssueStatus_STATUS_TYPE_INITIAL: {openIssue1.ID, openIssue2.ID},
			},
		},
	}

	for _, tc := range tests {
		suite.T().Run(tc.name, func(t *testing.T) {
			ctx := testutils.AuthorizeGRPC(tc.user.Identity)
			resp, err := client.ListBoardIssues(ctx, tc.req)

			if tc.expectedStatus != 0 {
				st, ok := status.FromError(err)
				require.True(t, ok)
				require.Equal(t, tc.expectedStatus, st.Code())
				return
			}
			require.NoError(t, err)

			// check columns
			require.Len(t, resp.Columns, len(tc.expectedIDs))
			for _, col := range resp.Columns {
				expected := tc.expectedIDs[col.StatusType]
				actual := getIssueIDs(col.Issues)
				require.ElementsMatch(t, expected, actual)
			}
		})
	}
}

func getIssueIDs(issues []*pb.Issue) []uint64 {
	return functools.Map(issues, func(issue *pb.Issue) uint64 {
		id, _ := grpc_marshalling.IDDirect(issue.Id)
		return id
	})
}

func findColumn(columns []*pb.BoardColumnResponse, status pb.IssueStatus_StatusType) *pb.BoardColumnResponse {
	for _, col := range columns {
		if col.StatusType == status {
			return col
		}
	}
	return nil
}

func getIDs(issues []*entities.Issue) []uint64 {
	return functools.Map(issues, func(issue *entities.Issue) uint64 { return issue.ID })
}
