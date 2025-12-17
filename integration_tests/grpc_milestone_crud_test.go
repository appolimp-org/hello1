package integrationtests

import (
	"common/functools"
	"common/testutils/yarequire"
	"common/utils"
	"context"
	"gitcore/internal/entities"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/testutils"
	pagination_pb "private_api/generated/yandex/cloud/pagination"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type makeMilestoneOptions struct {
	RepoID      uint64
	Name        string
	Slug        *string
	Description string
	StartDate   *time.Time
	Deadline    *time.Time
}

func (suite *RwApiTestSuite) makeMilestone(user *entities.User, opts *makeMilestoneOptions) *entities.Milestone {
	t := suite.T()

	milestoneID, err := suite.MilestoneService.Create(context.Background(), &entities.Milestone{
		RepoID:      opts.RepoID,
		Name:        opts.Name,
		Slug:        opts.Slug,
		Description: opts.Description,
		StartDate:   opts.StartDate,
		Deadline:    opts.Deadline,
		AuthorID:    user.ID,
		UpdatedBy:   user.ID,
	}, user)
	require.NoError(t, err)

	milestone, err := suite.MilestoneRepo.Get(context.Background(), milestoneID)
	require.NoError(t, err)

	return milestone
}

func (suite *RwApiTestSuite) TestGrpcCreateMilestone() {
	t := suite.T()
	client := pb.NewMilestoneServiceClient(suite.grpcClient)
	repoID, _, _ := suite.makeRepoWithVisibility(t, suite.users.Admin, entities.Visibilities.Internal)

	tt := []struct {
		name           string
		request        *pb.CreateMilestoneRequest
		user           *entities.User
		checkMilestone func(*testing.T, *pb.Milestone)
		expectedStatus codes.Code
	}{
		{
			name: "simple milestone",
			user: suite.users.Admin,
			request: &pb.CreateMilestoneRequest{
				Name:        "Simple milestone",
				Description: "Description",
			},
			checkMilestone: func(t *testing.T, milestone *pb.Milestone) {
				require.Equal(t, "Simple milestone", milestone.Name)
				require.Equal(t, "simple-milestone", milestone.Slug)
				require.Equal(t, "Description", milestone.Description)
			},
			expectedStatus: codes.OK,
		},
		{
			name: "milestone with dates",
			user: suite.users.Admin,
			request: &pb.CreateMilestoneRequest{
				Name:        "Milestone with dates",
				Description: "Description",
				StartDate:   timestamppb.New(time.Date(2050, 1, 1, 0, 0, 0, 0, time.UTC)),
				Deadline:    timestamppb.New(time.Date(2050, 10, 1, 0, 0, 0, 0, time.UTC)),
			},
			checkMilestone: func(t *testing.T, milestone *pb.Milestone) {
				require.NotNil(t, milestone.StartDate)
				require.NotNil(t, milestone.Deadline)
			},
		},
		{
			name: "milestone with specified slug",
			user: suite.users.Admin,
			request: &pb.CreateMilestoneRequest{
				Name: "My milestone with slug",
				Slug: utils.PtrFromValue("milestone-with-slug"),
			},
			checkMilestone: func(t *testing.T, milestone *pb.Milestone) {
				require.Equal(t, "My milestone with slug", milestone.Name)
				require.Equal(t, "milestone-with-slug", milestone.Slug)
			},
			expectedStatus: codes.OK,
		},
		{
			name: "milestone with same generated slug",
			user: suite.users.Admin,
			request: &pb.CreateMilestoneRequest{
				Name: "milestone-with-slug",
			},
			checkMilestone: func(t *testing.T, milestone *pb.Milestone) {
				require.Equal(t, "milestone-with-slug", milestone.Name)
				require.Equal(t, "milestone-with-slug-1", milestone.Slug)
			},
			expectedStatus: codes.OK,
		},
		{
			name: "milestone with invalid slug",
			user: suite.users.Admin,
			request: &pb.CreateMilestoneRequest{
				Name: "Milestone with invalid slug",
				Slug: utils.PtrFromValue("invalid slug with spaces"),
			},
			expectedStatus: codes.InvalidArgument,
		},
		{
			name: "permission denied",
			user: suite.users.Krosh,
			request: &pb.CreateMilestoneRequest{
				Name: "Milestone",
			},
			expectedStatus: codes.PermissionDenied,
		},
	}

	for _, tc := range tt {
		suite.T().Run(tc.name, func(t *testing.T) {
			ctx := testutils.AuthorizeGRPC(tc.user.Identity)
			tc.request.RepoId = grpc_marshalling.IDInverse(repoID)
			resp, err := client.Create(ctx, tc.request)
			yarequire.ProtoStatusEqual(t, tc.expectedStatus, err)
			if tc.expectedStatus != codes.OK {
				return
			}

			createdMilestone, err := grpc_marshalling.OperationResponse(resp, &pb.Milestone{})
			require.NoError(t, err)
			if tc.checkMilestone != nil {
				tc.checkMilestone(t, createdMilestone)
				require.Equal(t, grpc_marshalling.IDInverse(tc.user.ID), createdMilestone.AuthorId)
				require.Equal(t, grpc_marshalling.IDInverse(tc.user.ID), createdMilestone.UpdatedBy)
			}

			//yarequire.ProtoDumpFixture(t, createdMilestone)
			yarequire.ProtoCompareWithFixture(t, createdMilestone,
				protocmp.IgnoreFields(&pb.Milestone{}, "id", "created_at", "updated_at"),
			)
		})
	}
}

func (suite *RwApiTestSuite) TestGrpcGetMilestone() {
	t := suite.T()
	client := pb.NewMilestoneServiceClient(suite.grpcClient)
	repoID := suite.repos.Alpha.ID
	privateRepoID, _, _ := suite.makeRepoWithVisibility(t, suite.users.Admin, entities.Visibilities.Private)

	milestone := suite.makeMilestone(suite.users.Kopatych, &makeMilestoneOptions{
		RepoID:      repoID,
		Name:        "Test milestone",
		Description: "A test milestone",
	})
	privateMilestone := suite.makeMilestone(suite.users.Admin, &makeMilestoneOptions{
		RepoID: privateRepoID,
		Name:   "Private milestone",
	})

	tt := map[string]struct {
		request        *pb.GetMilestoneRequest
		user           *entities.User
		expectedName   string
		expectedStatus codes.Code
	}{
		"get by ID": {
			user: suite.users.Kopatych,
			request: &pb.GetMilestoneRequest{
				Id: grpc_marshalling.IDInverse(milestone.ID),
			},
			expectedName:   "Test milestone",
			expectedStatus: codes.OK,
		},
		"milestone not found by id": {
			user: suite.users.Admin,
			request: &pb.GetMilestoneRequest{
				Id: "123456789",
			},
			expectedStatus: codes.NotFound,
		},
		"permission denied": {
			user: suite.users.Kopatych,
			request: &pb.GetMilestoneRequest{
				Id: grpc_marshalling.IDInverse(privateMilestone.ID),
			},
			expectedStatus: codes.PermissionDenied,
		},
	}

	for name, tc := range tt {
		suite.T().Run(name, func(t *testing.T) {
			ctx := testutils.AuthorizeGRPC(tc.user.Identity)
			res, err := client.Get(ctx, tc.request)
			yarequire.ProtoStatusEqual(t, tc.expectedStatus, err)
			if tc.expectedStatus != codes.OK {
				return
			}

			//yarequire.ProtoDumpFixture(t, res)
			yarequire.ProtoCompareWithFixture(t, res,
				protocmp.IgnoreFields(&pb.Milestone{}, "id", "created_at", "updated_at"),
			)
		})
	}
}

func (suite *RwApiTestSuite) TestGrpcGetBulkMilestones() {
	client := pb.NewMilestoneServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)
	repoID := suite.repos.Alpha.ID

	milestones := []*entities.Milestone{
		suite.makeMilestone(suite.users.Admin, &makeMilestoneOptions{
			RepoID:      repoID,
			Name:        "First Milestone",
			Description: "First test milestone",
		}),
		suite.makeMilestone(suite.users.Kopatych, &makeMilestoneOptions{
			RepoID:      repoID,
			Name:        "Second Milestone",
			Description: "Second test milestone",
		}),
		suite.makeMilestone(suite.users.Krosh, &makeMilestoneOptions{
			RepoID:      repoID,
			Name:        "Third Milestone",
			Description: "Third test milestone",
		}),
	}
	milestonesPB := functools.Map(milestones, grpc_marshalling.EntityToPB.Milestone)

	tt := map[string]struct {
		request         *pb.GetBulkMilestonesRequest
		expectedResults []*pb.Milestone
		expectedStatus  codes.Code
	}{
		"get multiple milestones": {
			request: &pb.GetBulkMilestonesRequest{
				RepoId: grpc_marshalling.IDInverse(repoID),
				MilestoneIds: []string{
					grpc_marshalling.IDInverse(milestones[0].ID),
					grpc_marshalling.IDInverse(milestones[1].ID),
				},
			},
			expectedResults: []*pb.Milestone{
				milestonesPB[0],
				milestonesPB[1],
			},
			expectedStatus: codes.OK,
		},
		"get single milestone": {
			request: &pb.GetBulkMilestonesRequest{
				RepoId:       grpc_marshalling.IDInverse(repoID),
				MilestoneIds: []string{grpc_marshalling.IDInverse(milestones[2].ID)},
			},
			expectedResults: []*pb.Milestone{
				milestonesPB[2],
			},
			expectedStatus: codes.OK,
		},
		"get non-existent milestone": {
			request: &pb.GetBulkMilestonesRequest{
				RepoId:       grpc_marshalling.IDInverse(repoID),
				MilestoneIds: []string{"123456789"},
			},
			expectedResults: nil,
			expectedStatus:  codes.OK,
		},
		"empty milestone IDs": {
			request: &pb.GetBulkMilestonesRequest{
				RepoId:       grpc_marshalling.IDInverse(repoID),
				MilestoneIds: []string{},
			},
			expectedResults: nil,
			expectedStatus:  codes.InvalidArgument,
		},
	}

	for tn, tc := range tt {
		suite.T().Run(tn, func(t *testing.T) {
			resp, err := client.GetBulk(ctx, tc.request)
			yarequire.ProtoStatusEqual(t, tc.expectedStatus, err)

			if tc.expectedStatus != codes.OK {
				return
			}

			require.NoError(t, err)
			require.Len(t, resp.Milestones, len(tc.expectedResults))

			for i := range resp.Milestones {
				yarequire.ProtoEqual(t, tc.expectedResults[i], resp.Milestones[i])
			}
		})
	}
}

func (suite *RwApiTestSuite) TestGrpcListMilestones() {
	client := pb.NewMilestoneServiceClient(suite.grpcClient)
	repoID := suite.repos.Alpha.ID

	milestones := []*entities.Milestone{
		suite.makeMilestone(suite.users.Admin, &makeMilestoneOptions{
			RepoID:      repoID,
			Name:        "First Milestone",
			Description: "First test milestone",
		}),
		suite.makeMilestone(suite.users.Kopatych, &makeMilestoneOptions{
			RepoID:      repoID,
			Name:        "Second Milestone",
			Description: "Second test milestone",
		}),
	}
	milestonesPB := functools.Map(milestones, grpc_marshalling.EntityToPB.Milestone)

	tests := []*struct {
		name     string
		user     *entities.User
		args     *pb.ListMilestonesRequest
		expected pb.ListMilestonesResponse
		wantCode codes.Code
	}{
		{
			name: "list all milestones",
			user: suite.users.Admin,
			args: &pb.ListMilestonesRequest{
				RepoId: grpc_marshalling.IDInverse(repoID),
			},
			expected: pb.ListMilestonesResponse{
				Milestones: milestonesPB,
				Revision: &pb.Revision{
					Value: "2",
					Count: utils.PtrFromValue(int32(2)),
				},
				Total: utils.PtrFromValue(int32(2)),
			},
		},
		{
			name: "list with sort",
			user: suite.users.Admin,
			args: &pb.ListMilestonesRequest{
				RepoId: grpc_marshalling.IDInverse(repoID),
				SortBy: []*pagination_pb.SortOption{
					{
						Column:    "created_at",
						Direction: pagination_pb.SortOption_ASC,
					},
				},
			},
			expected: pb.ListMilestonesResponse{
				Milestones: milestonesPB,
				Revision: &pb.Revision{
					Value: "2",
					Count: utils.PtrFromValue(int32(2)),
				},
				Total: utils.PtrFromValue(int32(2)),
			},
		},
	}

	for _, tt := range tests {
		suite.T().Run(tt.name, func(t *testing.T) {
			ctx := testutils.AuthorizeGRPC(tt.user.Identity)
			resp, err := client.List(ctx, tt.args)

			require.NoError(t, err)
			require.Len(t, resp.Milestones, len(tt.expected.Milestones))
			yarequire.ProtoEqual(t, &tt.expected, resp)
		})
	}
}

func (suite *RwApiTestSuite) TestGrpcListMilestonesWithFilter() {
	client := pb.NewMilestoneServiceClient(suite.grpcClient)
	repoID := suite.repos.Alpha.ID

	suite.makeMilestone(suite.users.Admin, &makeMilestoneOptions{
		RepoID: repoID,
		Name:   "Milestone1",
	})
	closedMilestone := suite.makeMilestone(suite.users.Admin, &makeMilestoneOptions{
		RepoID:      repoID,
		Name:        "Milestone2",
		Description: "A closed milestone",
	})
	_, err := client.Update(testutils.AuthorizeGRPC(suite.users.Admin.Identity), &pb.UpdateMilestoneRequest{
		Id:         grpc_marshalling.IDInverse(closedMilestone.ID),
		Status:     pb.Milestone_STATUS_CLOSED,
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"status"}},
	})
	require.NoError(suite.T(), err)

	tests := map[string]struct {
		filter         *pagination_pb.Filter
		expectedCount  int
		expectedStatus codes.Code
	}{
		"filter by status closed": {
			filter: &pagination_pb.Filter{
				Filter: &pagination_pb.Filter_Predicate{
					Predicate: &pagination_pb.Predicate{
						Field:    "status",
						Operator: pagination_pb.Operator_OPERATOR_EQ,
						Operand: &pagination_pb.Predicate_StringValue{
							StringValue: pb.Milestone_STATUS_CLOSED.String(),
						},
					},
				},
			},
			expectedCount:  1,
			expectedStatus: codes.OK,
			// Matches: 2
		},
	}

	for name, tc := range tests {
		suite.T().Run(name, func(t *testing.T) {
			ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)
			resp, err := client.List(ctx, &pb.ListMilestonesRequest{
				RepoId: grpc_marshalling.IDInverse(repoID),
				Filter: tc.filter,
			})
			yarequire.ProtoStatusEqual(t, tc.expectedStatus, err)
			require.Len(t, resp.Milestones, tc.expectedCount)
			require.NoError(t, err)

			//yarequire.ProtoDumpFixture(t, resp)
			yarequire.ProtoCompareWithFixture(t, resp,
				protocmp.IgnoreFields(&pb.Milestone{}, "id", "created_at", "updated_at"),
			)
		})
	}
}

func (suite *RwApiTestSuite) TestGrpcUpdateMilestone() {
	t := suite.T()
	client := pb.NewMilestoneServiceClient(suite.grpcClient)
	repoID, _, _ := suite.makeRandomRepo(t, suite.users.Admin)

	createDefaultMilestone := func() uint64 {
		return suite.makeMilestone(suite.users.Admin, &makeMilestoneOptions{
			RepoID:      repoID,
			Name:        "Milestone",
			Slug:        utils.PtrFromValue(suite.mustGenUniqueSlug()),
			Description: "Description",
		}).ID
	}
	existingMilestone := suite.makeMilestone(suite.users.Admin, &makeMilestoneOptions{
		RepoID: repoID,
		Name:   "Existing milestone",
		Slug:   utils.PtrFromValue("existing-milestone"),
	})

	testCases := map[string]struct {
		updateRequest  *pb.UpdateMilestoneRequest
		checkMilestone func(*testing.T, *pb.Milestone)
		expectedStatus codes.Code
	}{
		"update milestone name": {
			updateRequest: &pb.UpdateMilestoneRequest{
				Name:       "Updated Milestone",
				UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"name"}},
			},
			checkMilestone: func(t *testing.T, updated *pb.Milestone) {
				require.Equal(t, "Updated Milestone", updated.Name)
			},
			expectedStatus: codes.OK,
		},
		"update milestone slug": {
			updateRequest: &pb.UpdateMilestoneRequest{
				Slug:       "updated-slug",
				UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"slug"}},
			},
			checkMilestone: func(t *testing.T, milestone *pb.Milestone) {
				require.Equal(t, "updated-slug", milestone.Slug)
			},
			expectedStatus: codes.OK,
		},
		"update milestone slug to existing label slug": {
			updateRequest: &pb.UpdateMilestoneRequest{
				Slug:       *existingMilestone.Slug,
				UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"slug"}},
			},
			expectedStatus: codes.FailedPrecondition,
		},
		"update milestone description": {
			updateRequest: &pb.UpdateMilestoneRequest{
				Description: "Updated Description",
				UpdateMask:  &fieldmaskpb.FieldMask{Paths: []string{"description"}},
			},
			checkMilestone: func(t *testing.T, updated *pb.Milestone) {
				require.Equal(t, "Updated Description", updated.Description)
			},
			expectedStatus: codes.OK,
		},
		"update milestone start date": {
			updateRequest: &pb.UpdateMilestoneRequest{
				StartDate:  timestamppb.New(time.Date(2055, 1, 1, 0, 0, 0, 0, time.UTC)),
				UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"start_date"}},
			},
			checkMilestone: func(t *testing.T, updated *pb.Milestone) {
				require.Equal(t, time.Date(2055, 1, 1, 0, 0, 0, 0, time.UTC), updated.StartDate.AsTime())
				require.Nil(t, updated.Deadline)
			},
			expectedStatus: codes.OK,
		},
		"clear milestone start date": {
			updateRequest: &pb.UpdateMilestoneRequest{
				StartDate:  nil,
				UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"start_date"}},
			},
			checkMilestone: func(t *testing.T, updated *pb.Milestone) {
				require.Nil(t, updated.StartDate)
			},
			expectedStatus: codes.OK,
		},
		"update milestone deadline": {
			updateRequest: &pb.UpdateMilestoneRequest{
				Deadline:   timestamppb.New(time.Date(2060, 1, 1, 0, 0, 0, 0, time.UTC)),
				UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"deadline"}},
			},
			checkMilestone: func(t *testing.T, updated *pb.Milestone) {
				require.Nil(t, updated.StartDate)
				require.Equal(t, time.Date(2060, 1, 1, 0, 0, 0, 0, time.UTC), updated.Deadline.AsTime())
			},
			expectedStatus: codes.OK,
		},
		"clear milestone deadline": {
			updateRequest: &pb.UpdateMilestoneRequest{
				Deadline:   nil,
				UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"deadline"}},
			},
			checkMilestone: func(t *testing.T, updated *pb.Milestone) {
				require.Nil(t, updated.Deadline)
			},
			expectedStatus: codes.OK,
		},
		"update milestone status": {
			updateRequest: &pb.UpdateMilestoneRequest{
				Status:     pb.Milestone_STATUS_CLOSED,
				UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"status"}},
			},
			checkMilestone: func(t *testing.T, updated *pb.Milestone) {
				require.Equal(t, pb.Milestone_STATUS_CLOSED, updated.Status)
			},
			expectedStatus: codes.OK,
		},
	}

	for name, tc := range testCases {
		suite.T().Run(name, func(t *testing.T) {
			ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)

			defaultMilestoneID := createDefaultMilestone()
			if tc.updateRequest.Id == "" {
				tc.updateRequest.Id = grpc_marshalling.IDInverse(defaultMilestoneID)
			}
			resp, err := client.Update(ctx, tc.updateRequest)
			yarequire.ProtoStatusEqual(t, tc.expectedStatus, err)

			if tc.expectedStatus != codes.OK {
				return
			}

			updatedMilestone, err := grpc_marshalling.OperationResponse(resp, &pb.Milestone{})
			require.NoError(t, err)
			if tc.checkMilestone != nil {
				tc.checkMilestone(t, updatedMilestone)
			}
			//yarequire.ProtoDumpFixture(t, updatedMilestone)
			yarequire.ProtoCompareWithFixture(t, updatedMilestone,
				protocmp.IgnoreFields(&pb.Milestone{}, "id", "slug", "created_at", "updated_at"),
			)

			fetchedMilestone, err := client.Get(ctx, &pb.GetMilestoneRequest{
				Id: updatedMilestone.Id,
			})
			yarequire.ProtoStatusEqual(t, codes.OK, err)
			require.NoError(t, err)
			if tc.checkMilestone != nil {
				tc.checkMilestone(t, updatedMilestone)
			}
			yarequire.ProtoCompareWithFixture(t, fetchedMilestone,
				protocmp.IgnoreFields(&pb.Milestone{}, "id", "slug", "created_at", "updated_at"),
			)
		})
	}
}

func (suite *RwApiTestSuite) TestGrpcDeleteMilestone() {
	t := suite.T()
	client := pb.NewMilestoneServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)

	t.Run("delete existing milestone", func(t *testing.T) {
		milestone := suite.makeMilestone(suite.users.Admin, &makeMilestoneOptions{
			RepoID:      suite.repos.Alpha.ID,
			Name:        "Milestone to Delete",
			Description: "This milestone will be deleted",
		})
		milestoneID := grpc_marshalling.IDInverse(milestone.ID)

		_, err := client.Get(ctx, &pb.GetMilestoneRequest{
			Id: milestoneID,
		})
		yarequire.ProtoStatusEqual(t, codes.OK, err)

		_, err = client.Delete(ctx, &pb.DeleteMilestoneRequest{Id: milestoneID})
		yarequire.ProtoStatusEqual(t, codes.OK, err)

		_, err = client.Get(ctx, &pb.GetMilestoneRequest{
			Id: milestoneID,
		})
		yarequire.ProtoStatusEqual(t, codes.NotFound, err)
	})
}
