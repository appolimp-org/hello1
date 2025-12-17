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

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

type makeLabelOptions struct {
	ID     uint64
	RepoID uint64
	Name   string
	Slug   *string
	Color  string
}

func (suite *RwApiTestSuite) makeLabel(user *entities.User, opts *makeLabelOptions) *entities.Label {
	t := suite.T()

	if opts.Color == "" {
		opts.Color = entities.PresetLabelColors.Grey
	}
	labelID, err := suite.LabelService.Create(context.Background(), &entities.Label{
		ID:        opts.ID,
		RepoID:    opts.RepoID,
		Name:      opts.Name,
		Slug:      opts.Slug,
		Color:     opts.Color,
		AuthorID:  user.ID,
		UpdatedBy: user.ID,
	}, user)
	require.NoError(t, err)

	label, err := suite.LabelRepo.Get(context.Background(), labelID)
	require.NoError(t, err)

	return label
}

func (suite *RwApiTestSuite) TestGrpcCreateLabel() {
	t := suite.T()
	client := pb.NewLabelServiceClient(suite.grpcClient)
	repoID, _, _ := suite.makeRepoWithVisibility(t, suite.users.Admin, entities.Visibilities.Internal)

	tt := []struct {
		name           string
		request        *pb.CreateLabelRequest
		user           *entities.User
		checkLabel     func(*testing.T, *pb.Label)
		expectedStatus codes.Code
	}{
		{
			name: "simple label",
			user: suite.users.Admin,
			request: &pb.CreateLabelRequest{
				Name:  "Simple label",
				Color: entities.PresetLabelColors.Red,
			},
			checkLabel: func(t *testing.T, label *pb.Label) {
				require.Equal(t, "Simple label", label.Name)
				require.Equal(t, "simple-label", label.Slug)
				require.Equal(t, entities.PresetLabelColors.Red, label.GetColor())
			},
			expectedStatus: codes.OK,
		},
		{
			name: "label with specified slug",
			user: suite.users.Admin,
			request: &pb.CreateLabelRequest{
				Name:  "My label with slug",
				Slug:  utils.PtrFromValue("label-with-slug"),
				Color: entities.PresetLabelColors.Green,
			},
			checkLabel: func(t *testing.T, label *pb.Label) {
				require.Equal(t, "My label with slug", label.Name)
				require.Equal(t, "label-with-slug", label.Slug)
				require.Equal(t, entities.PresetLabelColors.Green, label.GetColor())
			},
			expectedStatus: codes.OK,
		},
		{
			name: "label with same generated slug",
			user: suite.users.Admin,
			request: &pb.CreateLabelRequest{
				Name:  "label-with-slug",
				Color: entities.PresetLabelColors.Green,
			},
			checkLabel: func(t *testing.T, label *pb.Label) {
				require.Equal(t, "label-with-slug", label.Name)
				require.Equal(t, "label-with-slug-1", label.Slug)
				require.Equal(t, entities.PresetLabelColors.Green, label.GetColor())
			},
			expectedStatus: codes.OK,
		},
		{
			name: "label with invalid slug",
			user: suite.users.Admin,
			request: &pb.CreateLabelRequest{
				Name:  "Label with invalid slug",
				Slug:  utils.PtrFromValue("invalid slug with spaces"),
				Color: entities.PresetLabelColors.Green,
			},
			expectedStatus: codes.InvalidArgument,
		},
		{
			name: "permission denied",
			user: suite.users.Krosh,
			request: &pb.CreateLabelRequest{
				Name:  "Label",
				Color: entities.PresetLabelColors.Grey,
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

			createdLabel, err := grpc_marshalling.OperationResponse(resp, &pb.Label{})
			require.NoError(t, err)
			if tc.checkLabel != nil {
				tc.checkLabel(t, createdLabel)
				require.Equal(t, grpc_marshalling.IDInverse(tc.user.ID), createdLabel.AuthorId)
				require.Equal(t, grpc_marshalling.IDInverse(tc.user.ID), createdLabel.UpdatedBy)
			}

			//yarequire.ProtoDumpFixture(t, createdLabel)
			yarequire.ProtoCompareWithFixture(t, createdLabel,
				protocmp.IgnoreFields(&pb.Label{}, "id", "created_at", "updated_at"),
			)
		})
	}
}

func (suite *RwApiTestSuite) TestGrpcCreateDuplicateLabel() {
	t := suite.T()
	client := pb.NewLabelServiceClient(suite.grpcClient)
	repoID1, _, _ := suite.makeRandomRepo(t, suite.users.Admin)
	repoID2, _, _ := suite.makeRandomRepo(t, suite.users.Admin)

	labelName := "DuplicateLabel"
	ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)

	// Create the first label in repo1 - should succeed
	resp, err := client.Create(ctx, &pb.CreateLabelRequest{
		RepoId: grpc_marshalling.IDInverse(repoID1),
		Name:   labelName,
		Color:  entities.PresetLabelColors.Grey,
	})
	yarequire.ProtoStatusEqual(t, codes.OK, err)

	createdLabel1, err := grpc_marshalling.OperationResponse(resp, &pb.Label{})
	require.NoError(t, err)
	require.Equal(t, labelName, createdLabel1.Name)

	// Attempt to create a duplicate label in the same repository (repo1) - should fail
	_, err = client.Create(ctx, &pb.CreateLabelRequest{
		RepoId: grpc_marshalling.IDInverse(repoID1),
		Name:   labelName,
		Color:  entities.PresetLabelColors.Grey,
	})
	yarequire.ProtoStatusEqual(t, codes.AlreadyExists, err)

	// Create the same label name in a different repository (repo2) - should succeed
	_, err = client.Create(ctx, &pb.CreateLabelRequest{
		RepoId: grpc_marshalling.IDInverse(repoID2),
		Name:   labelName,
		Color:  entities.PresetLabelColors.Grey,
	})
	yarequire.ProtoStatusEqual(t, codes.OK, err)

	createdLabel2, err := grpc_marshalling.OperationResponse(resp, &pb.Label{})
	require.NoError(t, err)
	require.Equal(t, labelName, createdLabel2.Name)

	respList, err := client.List(ctx, &pb.ListLabelsRequest{
		RepoId: grpc_marshalling.IDInverse(repoID1),
	})
	require.NoError(t, err)
	require.Len(t, respList.Labels, 1)
	require.Equal(t, labelName, respList.Labels[0].Name)

	respList, err = client.List(ctx, &pb.ListLabelsRequest{
		RepoId: grpc_marshalling.IDInverse(repoID2),
	})
	require.NoError(t, err)
	require.Len(t, respList.Labels, 1)
	require.Equal(t, labelName, respList.Labels[0].Name)
}

func (suite *RwApiTestSuite) TestGrpcGetLabel() {
	client := pb.NewLabelServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)
	repoID := suite.repos.Alpha.ID

	label := suite.makeLabel(suite.users.Kopatych, &makeLabelOptions{
		RepoID: repoID,
		Name:   "Test Label",
	})

	tt := map[string]struct {
		request        *pb.GetLabelRequest
		expectedName   string
		expectedStatus codes.Code
	}{
		"get by ID": {
			request: &pb.GetLabelRequest{
				Label: &pb.GetLabelRequest_Id{Id: grpc_marshalling.IDInverse(label.ID)},
			},
			expectedName:   "Test Label",
			expectedStatus: codes.OK,
		},
		"label not found by id": {
			request: &pb.GetLabelRequest{
				Label: &pb.GetLabelRequest_Id{Id: "123456789"},
			},
			expectedStatus: codes.NotFound,
		},
		"get by name": {
			request: &pb.GetLabelRequest{
				Label: &pb.GetLabelRequest_FullName{FullName: &pb.LabelIdentity{
					RepoId:    grpc_marshalling.IDInverse(repoID),
					LabelName: label.Name,
				}},
			},
			expectedName:   "Test Label",
			expectedStatus: codes.OK,
		},
		"label not found by name": {
			request: &pb.GetLabelRequest{
				Label: &pb.GetLabelRequest_FullName{FullName: &pb.LabelIdentity{
					RepoId:    grpc_marshalling.IDInverse(repoID),
					LabelName: "Not existing label",
				}},
			},
			expectedStatus: codes.NotFound,
		},
	}

	for name, tc := range tt {
		suite.T().Run(name, func(t *testing.T) {
			res, err := client.Get(ctx, tc.request)
			yarequire.ProtoStatusEqual(t, tc.expectedStatus, err)
			if tc.expectedStatus != codes.OK {
				return
			}

			//yarequire.ProtoDumpFixture(t, res)
			yarequire.ProtoCompareWithFixture(t, res,
				protocmp.IgnoreFields(&pb.Label{}, "id", "created_at", "updated_at"),
			)
		})
	}
}

func (suite *RwApiTestSuite) TestGrpcGetBulkLabels() {
	client := pb.NewLabelServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)
	repoID := suite.repos.Alpha.ID

	labels := []*entities.Label{
		suite.makeLabel(suite.users.Admin, &makeLabelOptions{
			RepoID: repoID,
			Name:   "First Label",
		}),
		suite.makeLabel(suite.users.Kopatych, &makeLabelOptions{
			RepoID: repoID,
			Name:   "Second Label",
		}),
		suite.makeLabel(suite.users.Krosh, &makeLabelOptions{
			RepoID: repoID,
			Name:   "Third Label",
		}),
	}
	labelsPB := functools.Map(labels, grpc_marshalling.EntityToPB.Label)

	tt := map[string]struct {
		request        *pb.GetBulkLabelsRequest
		expectedLabels []*pb.Label
		expectedStatus codes.Code
	}{
		"get multiple labels": {
			request: &pb.GetBulkLabelsRequest{
				RepoId: grpc_marshalling.IDInverse(repoID),
				LabelIds: []string{
					grpc_marshalling.IDInverse(labels[0].ID),
					grpc_marshalling.IDInverse(labels[1].ID),
				},
			},
			expectedLabels: []*pb.Label{
				labelsPB[0],
				labelsPB[1],
			},
			expectedStatus: codes.OK,
		},
		"get single label": {
			request: &pb.GetBulkLabelsRequest{
				RepoId:   grpc_marshalling.IDInverse(repoID),
				LabelIds: []string{grpc_marshalling.IDInverse(labels[2].ID)},
			},
			expectedLabels: []*pb.Label{
				labelsPB[2],
			},
			expectedStatus: codes.OK,
		},
		"get not existing label": {
			request: &pb.GetBulkLabelsRequest{
				RepoId:   grpc_marshalling.IDInverse(repoID),
				LabelIds: []string{"123456789"},
			},
			expectedLabels: nil,
			expectedStatus: codes.OK,
		},
		"empty label IDs": {
			request: &pb.GetBulkLabelsRequest{
				RepoId:   grpc_marshalling.IDInverse(repoID),
				LabelIds: []string{},
			},
			expectedLabels: nil,
			expectedStatus: codes.InvalidArgument,
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
			require.Len(t, resp.Labels, len(tc.expectedLabels))

			for i := range resp.Labels {
				yarequire.ProtoEqual(t, tc.expectedLabels[i], resp.Labels[i])
			}
		})
	}
}

func (suite *RwApiTestSuite) TestGrpcListLabels() {
	client := pb.NewLabelServiceClient(suite.grpcClient)
	repoID := suite.repos.Alpha.ID

	labels := []*entities.Label{
		suite.makeLabel(suite.users.Admin, &makeLabelOptions{
			RepoID: repoID,
			Name:   "First Label",
		}),
		suite.makeLabel(suite.users.Kopatych, &makeLabelOptions{
			RepoID: repoID,
			Name:   "Second Label",
		}),
	}
	labelsPB := functools.Map(labels, grpc_marshalling.EntityToPB.Label)

	tests := []*struct {
		name     string
		user     *entities.User
		args     *pb.ListLabelsRequest
		expected pb.ListLabelsResponse
		wantCode codes.Code
	}{
		{
			name: "list all labels",
			user: suite.users.Admin,
			args: &pb.ListLabelsRequest{
				RepoId: grpc_marshalling.IDInverse(repoID),
			},
			expected: pb.ListLabelsResponse{
				Labels: labelsPB,
				Revision: &pb.Revision{
					Value: "2",
					Count: utils.PtrFromValue(int32(2)),
				},
			},
		},
		{
			name: "list with sort",
			user: suite.users.Admin,
			args: &pb.ListLabelsRequest{
				RepoId: grpc_marshalling.IDInverse(repoID),
				SortBy: []*pagination_pb.SortOption{
					{
						Column:    "created_at",
						Direction: pagination_pb.SortOption_ASC,
					},
				},
			},
			expected: pb.ListLabelsResponse{
				Labels: labelsPB,
				Revision: &pb.Revision{
					Value: "2",
					Count: utils.PtrFromValue(int32(2)),
				},
			},
		},
	}

	for _, tt := range tests {
		suite.T().Run(tt.name, func(t *testing.T) {
			ctx := testutils.AuthorizeGRPC(tt.user.Identity)
			resp, err := client.List(ctx, tt.args)

			require.NoError(t, err)
			require.Len(t, resp.Labels, len(tt.expected.Labels))
			yarequire.ProtoEqual(t, &tt.expected, resp)
		})
	}
}

func (suite *RwApiTestSuite) TestGrpcUpdateLabel() {
	t := suite.T()
	client := pb.NewLabelServiceClient(suite.grpcClient)
	repoID, _, _ := suite.makeRandomRepo(t, suite.users.Admin)

	createDefaultLabel := func() uint64 {
		return suite.makeLabel(suite.users.Admin, &makeLabelOptions{
			RepoID: repoID,
			Name:   suite.mustGenUniqueSlug(),
			Color:  entities.PresetLabelColors.Grey,
		}).ID
	}
	existingLabel := suite.makeLabel(suite.users.Admin, &makeLabelOptions{
		RepoID: repoID,
		Name:   "Existing Label",
		Slug:   utils.PtrFromValue("existing-label"),
	})

	testCases := map[string]struct {
		user           *entities.User
		updateRequest  *pb.UpdateLabelRequest
		checkLabel     func(*testing.T, *pb.Label)
		expectedStatus codes.Code
	}{
		"update label name": {
			updateRequest: &pb.UpdateLabelRequest{
				Name:       "Updated Label",
				UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"name"}},
			},
			checkLabel: func(t *testing.T, label *pb.Label) {
				require.Equal(t, "Updated Label", label.Name)
			},
			expectedStatus: codes.OK,
		},
		"update label name to existing label name": {
			updateRequest: &pb.UpdateLabelRequest{
				Name:       existingLabel.Name,
				UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"name"}},
			},
			expectedStatus: codes.AlreadyExists,
		},
		"update label slug": {
			updateRequest: &pb.UpdateLabelRequest{
				Slug:       "updated-slug",
				UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"slug"}},
			},
			checkLabel: func(t *testing.T, label *pb.Label) {
				require.Equal(t, "updated-slug", label.Slug)
			},
			expectedStatus: codes.OK,
		},
		"update label slug to existing label slug": {
			updateRequest: &pb.UpdateLabelRequest{
				Slug:       *existingLabel.Slug,
				UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"slug"}},
			},
			expectedStatus: codes.FailedPrecondition,
		},
		"update label color": {
			updateRequest: &pb.UpdateLabelRequest{
				Color:      entities.PresetLabelColors.Green,
				UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"color"}},
			},
			checkLabel: func(t *testing.T, label *pb.Label) {
				require.Equal(t, entities.PresetLabelColors.Green, label.GetColor())
			},
			expectedStatus: codes.OK,
		},
	}

	for name, tc := range testCases {
		suite.T().Run(name, func(t *testing.T) {
			defaultLabelID := createDefaultLabel()
			if tc.updateRequest.Id == "" {
				tc.updateRequest.Id = grpc_marshalling.IDInverse(defaultLabelID)
			}

			ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)
			resp, err := client.Update(ctx, tc.updateRequest)
			yarequire.ProtoStatusEqual(t, tc.expectedStatus, err)

			if tc.expectedStatus != codes.OK {
				return
			}

			updatedLabel, err := grpc_marshalling.OperationResponse(resp, &pb.Label{})
			require.NoError(t, err)
			if tc.checkLabel != nil {
				tc.checkLabel(t, updatedLabel)
			}
			//yarequire.ProtoDumpFixture(t, updatedLabel)
			yarequire.ProtoCompareWithFixture(t, updatedLabel,
				protocmp.IgnoreFields(&pb.Label{}, "id", "name", "slug", "created_at", "updated_at"),
			)

			fetchedLabel, err := client.Get(ctx, &pb.GetLabelRequest{
				Label: &pb.GetLabelRequest_Id{Id: updatedLabel.Id},
			})
			yarequire.ProtoStatusEqual(t, codes.OK, err)
			require.NoError(t, err)
			if tc.checkLabel != nil {
				tc.checkLabel(t, updatedLabel)
			}
			yarequire.ProtoCompareWithFixture(t, fetchedLabel,
				protocmp.IgnoreFields(&pb.Label{}, "id", "name", "slug", "created_at", "updated_at"),
			)
		})
	}
}

func (suite *RwApiTestSuite) TestGrpcDeleteLabel() {
	t := suite.T()
	client := pb.NewLabelServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)

	t.Run("delete existing label", func(t *testing.T) {
		label := suite.makeLabel(suite.users.Admin, &makeLabelOptions{
			RepoID: suite.repos.Alpha.ID,
			Name:   "Label to delete",
		})
		labelID := grpc_marshalling.IDInverse(label.ID)

		_, err := client.Get(ctx, &pb.GetLabelRequest{
			Label: &pb.GetLabelRequest_Id{Id: labelID},
		})
		yarequire.ProtoStatusEqual(t, codes.OK, err)

		_, err = client.Delete(ctx, &pb.DeleteLabelRequest{Id: labelID})
		yarequire.ProtoStatusEqual(t, codes.OK, err)

		_, err = client.Get(ctx, &pb.GetLabelRequest{
			Label: &pb.GetLabelRequest_Id{Id: labelID},
		})
		yarequire.ProtoStatusEqual(t, codes.NotFound, err)
	})

	t.Run("delete non-existent label", func(t *testing.T) {
		_, err := client.Delete(ctx, &pb.DeleteLabelRequest{Id: "123456789"})
		yarequire.ProtoStatusEqual(t, codes.NotFound, err)
	})

	t.Run("unauthenticated on delete anonymous", func(t *testing.T) {
		label := suite.makeLabel(suite.users.Admin, &makeLabelOptions{
			RepoID: suite.repos.Alpha.ID,
			Name:   "Protected label",
		})
		labelID := grpc_marshalling.IDInverse(label.ID)

		ctx := context.Background()
		_, err := client.Delete(ctx, &pb.DeleteLabelRequest{Id: labelID})
		yarequire.ProtoStatusEqual(t, codes.Unauthenticated, err)
	})
}
