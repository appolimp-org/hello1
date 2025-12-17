package integrationtests

import (
	"common/grpc/exceptions"
	"common/testutils/yarequire"
	"common/utils"
	"fmt"
	"gitcore/internal/entities"
	except "gitcore/internal/exceptions"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/revision"
	"gitcore/internal/testutils"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/testing/protocmp"
)

func (suite *RwApiTestSuite) TestGrpcCreateIssueLink() {
	t := suite.T()
	repoID, _, _ := suite.makeRandomRepo(t, suite.users.Admin)
	client := pb.NewIssueLinkServiceClient(suite.grpcClient)

	// Create issue for linking
	issues := make([]*entities.Issue, 5)
	for i := 0; i < 5; i++ {
		issues[i] = suite.makeIssue(suite.users.Admin, &makeIssueOptions{
			RepoID:     repoID,
			Title:      fmt.Sprintf("Issue%d", i+1),
			Visibility: entities.IssueVisibilities.Public,
		})
	}

	issue1 := grpc_marshalling.IDInverse(issues[0].ID)
	issue2 := grpc_marshalling.IDInverse(issues[1].ID)
	issue3 := grpc_marshalling.IDInverse(issues[2].ID)
	issue4 := grpc_marshalling.IDInverse(issues[3].ID)
	issue5 := grpc_marshalling.IDInverse(issues[4].ID)

	tests := []struct {
		name           string
		user           *entities.User
		request        *pb.CreateIssueLinkRequest
		checkLink      func(t *testing.T, link *pb.IssueLink)
		expectedStatus codes.Code
		expectedError  *exceptions.ExceptionTemplate
	}{
		{
			name: "relates",
			user: suite.users.Admin,
			request: &pb.CreateIssueLinkRequest{
				LeftIssueId:  issue1,
				RightIssueId: issue2,
				LinkType:     pb.IssueLink_LINK_TYPE_RELATES,
			},
			checkLink: func(t *testing.T, link *pb.IssueLink) {
				require.Equal(t, issue1, link.LeftIssueId)
				require.Equal(t, issue2, link.RightIssueId)
				require.Equal(t, pb.IssueLink_LINK_TYPE_RELATES, link.LinkType)
				require.Equal(t, grpc_marshalling.IDInverse(suite.users.Admin.ID), link.AuthorId)
			},
			expectedStatus: codes.OK,
		},
		{
			name: "duplicate link",
			user: suite.users.Admin,
			request: &pb.CreateIssueLinkRequest{
				LeftIssueId:  issue1,
				RightIssueId: issue2,
				LinkType:     pb.IssueLink_LINK_TYPE_RELATES,
			},
			expectedStatus: codes.AlreadyExists,
			expectedError:  except.IssueLinkAlreadyExists,
		},
		{
			name: "duplicate another type",
			user: suite.users.Admin,
			request: &pb.CreateIssueLinkRequest{
				LeftIssueId:  issue1,
				RightIssueId: issue2,
				LinkType:     pb.IssueLink_LINK_TYPE_DEPENDS_ON,
			},
			expectedStatus: codes.AlreadyExists,
			expectedError:  except.IssueLinkAlreadyExists,
		},
		{
			name: "self link",
			user: suite.users.Admin,
			request: &pb.CreateIssueLinkRequest{
				LeftIssueId:  issue1,
				RightIssueId: issue1,
				LinkType:     pb.IssueLink_LINK_TYPE_RELATES,
			},
			expectedStatus: codes.InvalidArgument,
			expectedError:  except.SelfIssueLinkNotAllowed,
		},
		{
			name: "swapped issues",
			user: suite.users.Admin,
			request: &pb.CreateIssueLinkRequest{
				LeftIssueId:  issue2,
				RightIssueId: issue1,
				LinkType:     pb.IssueLink_LINK_TYPE_RELATES,
			},
			expectedStatus: codes.AlreadyExists,
			expectedError:  except.IssueLinkAlreadyExists,
		},
		{
			name: "no rights",
			user: suite.users.Slowpoke,
			request: &pb.CreateIssueLinkRequest{
				LeftIssueId:  issue1,
				RightIssueId: issue2,
				LinkType:     pb.IssueLink_LINK_TYPE_RELATES,
			},
			expectedStatus: codes.PermissionDenied,
		},
		{
			name: "depends on",
			user: suite.users.Admin,
			request: &pb.CreateIssueLinkRequest{
				LeftIssueId:  issue1,
				RightIssueId: issue3,
				LinkType:     pb.IssueLink_LINK_TYPE_DEPENDS_ON,
			},
			expectedStatus: codes.OK,
		},
		{
			name: "blocks",
			user: suite.users.Admin,
			request: &pb.CreateIssueLinkRequest{
				LeftIssueId:  issue2,
				RightIssueId: issue3,
				LinkType:     pb.IssueLink_LINK_TYPE_BLOCKS,
			},
			expectedStatus: codes.OK,
		},
		{
			name: "parent with first child",
			user: suite.users.Admin,
			request: &pb.CreateIssueLinkRequest{
				LeftIssueId:  issue4,
				RightIssueId: issue1,
				LinkType:     pb.IssueLink_LINK_TYPE_PARENT,
			},
			expectedStatus: codes.OK,
		},
		{
			name: "parent with second child",
			user: suite.users.Admin,
			request: &pb.CreateIssueLinkRequest{
				LeftIssueId:  issue2,
				RightIssueId: issue4,
				LinkType:     pb.IssueLink_LINK_TYPE_CHILD,
			},
			expectedStatus: codes.OK,
		},
		{
			name: "try to create second parent",
			user: suite.users.Admin,
			request: &pb.CreateIssueLinkRequest{
				LeftIssueId:  issue5,
				RightIssueId: issue1,
				LinkType:     pb.IssueLink_LINK_TYPE_PARENT,
			},
			expectedStatus: codes.AlreadyExists,
			expectedError:  except.IssueHasParentAlready,
		},
		{
			name: "try to create second parent from child",
			user: suite.users.Admin,
			request: &pb.CreateIssueLinkRequest{
				LeftIssueId:  issue1,
				RightIssueId: issue5,
				LinkType:     pb.IssueLink_LINK_TYPE_CHILD,
			},
			expectedStatus: codes.AlreadyExists,
			expectedError:  except.IssueHasParentAlready,
		},
		{
			name: "duplicates",
			user: suite.users.Admin,
			request: &pb.CreateIssueLinkRequest{
				LeftIssueId:  issue1,
				RightIssueId: issue5,
				LinkType:     pb.IssueLink_LINK_TYPE_DUPLICATES,
			},
			expectedStatus: codes.OK,
		},
		{
			name: "duplicated by",
			user: suite.users.Admin,
			request: &pb.CreateIssueLinkRequest{
				LeftIssueId:  issue5,
				RightIssueId: issue2,
				LinkType:     pb.IssueLink_LINK_TYPE_DUPLICATED_BY,
			},
			expectedStatus: codes.OK,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := testutils.AuthorizeGRPC(tc.user.Identity)
			resp, err := client.Create(ctx, tc.request)
			yarequire.ProtoStatusEqual(t, tc.expectedStatus, err)
			if tc.expectedError != nil {
				yarequire.ProtoExceptionTemplate(t, err, tc.expectedError)
			}
			if tc.expectedStatus != codes.OK {
				return
			}
			createdLink, err := grpc_marshalling.OperationResponse(resp, &pb.IssueLink{})
			require.NoError(t, err)
			if tc.checkLink != nil {
				tc.checkLink(t, createdLink)
				require.Equal(t, grpc_marshalling.IDInverse(tc.user.ID), createdLink.AuthorId)
				require.Equal(t, grpc_marshalling.IDInverse(tc.user.ID), createdLink.UpdatedBy)
			}
			//yarequire.ProtoDumpFixture(t, createdLink)
			yarequire.ProtoCompareWithFixture(t, createdLink,
				protocmp.IgnoreFields(&pb.IssueLink{}, "id", "left_issue_id", "right_issue_id", "created_at", "updated_at"),
			)
		})
	}
}

func (suite *RwApiTestSuite) TestGrpcListIssueLinks() {
	t := suite.T()
	repoID, _, _ := suite.makeRandomRepo(t, suite.users.Admin)
	client := pb.NewIssueLinkServiceClient(suite.grpcClient)

	// Create issue for linking
	issues := make([]*entities.Issue, 5)
	for i := 0; i < 5; i++ {
		issues[i] = suite.makeIssue(suite.users.Admin, &makeIssueOptions{
			RepoID:     repoID,
			Title:      fmt.Sprintf("Issue%d", i+1),
			Visibility: entities.IssueVisibilities.Public,
		})
	}

	issue1 := grpc_marshalling.IDInverse(issues[0].ID)
	issue2 := grpc_marshalling.IDInverse(issues[1].ID)
	issue3 := grpc_marshalling.IDInverse(issues[2].ID)
	issue4 := grpc_marshalling.IDInverse(issues[3].ID)
	issue5 := grpc_marshalling.IDInverse(issues[4].ID)

	// relates
	link12, link21 := suite.createLink(t, client, &pb.CreateIssueLinkRequest{
		LeftIssueId:  issue1,
		RightIssueId: issue2,
		LinkType:     pb.IssueLink_LINK_TYPE_RELATES,
	})

	// blocks - depends_on
	link13, link31 := suite.createLink(t, client, &pb.CreateIssueLinkRequest{
		LeftIssueId:  issue1,
		RightIssueId: issue3,
		LinkType:     pb.IssueLink_LINK_TYPE_BLOCKS,
	})
	link23, link32 := suite.createLink(t, client, &pb.CreateIssueLinkRequest{
		LeftIssueId:  issue2,
		RightIssueId: issue3,
		LinkType:     pb.IssueLink_LINK_TYPE_DEPENDS_ON,
	})

	// parent - child
	link41, link14 := suite.createLink(t, client, &pb.CreateIssueLinkRequest{
		LeftIssueId:  issue4,
		RightIssueId: issue1,
		LinkType:     pb.IssueLink_LINK_TYPE_PARENT,
	})
	link24, link42 := suite.createLink(t, client, &pb.CreateIssueLinkRequest{
		LeftIssueId:  issue2,
		RightIssueId: issue4,
		LinkType:     pb.IssueLink_LINK_TYPE_CHILD,
	})

	// duplicates
	link15, link51 := suite.createLink(t, client, &pb.CreateIssueLinkRequest{
		LeftIssueId:  issue1,
		RightIssueId: issue5,
		LinkType:     pb.IssueLink_LINK_TYPE_DUPLICATES,
	})
	link53, link35 := suite.createLink(t, client, &pb.CreateIssueLinkRequest{
		LeftIssueId:  issue5,
		RightIssueId: issue3,
		LinkType:     pb.IssueLink_LINK_TYPE_DUPLICATED_BY,
	})

	tt := map[string]struct {
		user     *entities.User
		request  *pb.ListIssueLinksRequest
		expected *pb.ListIssueLinksResponse
		code     codes.Code
	}{
		"list links for issue 1": {
			user: suite.users.Admin,
			request: &pb.ListIssueLinksRequest{
				IssueId: issue1,
			},
			expected: &pb.ListIssueLinksResponse{
				Links:    []*pb.IssueLink{link12, link13, link14, link15},
				Revision: &pb.Revision{Value: "5", Count: utils.PtrFromValue(int32(4))},
			},
			code: codes.OK,
		},
		"list links for issue 2": {
			user: suite.users.Admin,
			request: &pb.ListIssueLinksRequest{
				IssueId: issue2,
			},
			expected: &pb.ListIssueLinksResponse{
				Links:    []*pb.IssueLink{link21, link23, link24},
				Revision: &pb.Revision{Value: "4", Count: utils.PtrFromValue(int32(3))},
			},
			code: codes.OK,
		},
		"list links for issue 3": {
			user: suite.users.Admin,
			request: &pb.ListIssueLinksRequest{
				IssueId: issue3,
			},
			expected: &pb.ListIssueLinksResponse{
				Links:    []*pb.IssueLink{link31, link32, link35},
				Revision: &pb.Revision{Value: "4", Count: utils.PtrFromValue(int32(3))},
			},
			code: codes.OK,
		},
		"list links for issue 4": {
			user: suite.users.Admin,
			request: &pb.ListIssueLinksRequest{
				IssueId: issue4,
			},
			expected: &pb.ListIssueLinksResponse{
				Links:    []*pb.IssueLink{link41, link42},
				Revision: &pb.Revision{Value: "3", Count: utils.PtrFromValue(int32(2))},
			},
			code: codes.OK,
		},
		"list links for issue 5": {
			user: suite.users.Admin,
			request: &pb.ListIssueLinksRequest{
				IssueId: issue5,
			},
			expected: &pb.ListIssueLinksResponse{
				Links:    []*pb.IssueLink{link51, link53},
				Revision: &pb.Revision{Value: "3", Count: utils.PtrFromValue(int32(2))},
			},
			code: codes.OK,
		},
	}

	for tn, tc := range tt {
		t.Run(tn, func(t *testing.T) {
			ctx := testutils.AuthorizeGRPC(tc.user.Identity)
			resp, err := client.List(ctx, tc.request)
			yarequire.ProtoStatusEqual(t, tc.code, err)
			if tc.code != codes.OK {
				return
			}

			require.NoError(t, err)
			require.Len(t, resp.Links, len(tc.expected.Links))
			yarequire.ProtoCmp(t, tc.expected, resp)

			// rev sync
			issueID, _ := grpc_marshalling.IDDirect(tc.request.IssueId)
			count, err := suite.RevSyncer.ComputeCount(ctx, revision.Issue(issueID).IssueLinks)
			require.NoError(t, err)
			require.Equal(t, *tc.expected.Revision.Count, int32(count))
		})
	}
}

func (suite *RwApiTestSuite) createLink(t *testing.T, client pb.IssueLinkServiceClient, req *pb.CreateIssueLinkRequest) (*pb.IssueLink, *pb.IssueLink) {
	ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)
	resp, err := client.Create(ctx, req)
	require.NoError(t, err)
	link, err := grpc_marshalling.OperationResponse(resp, &pb.IssueLink{})
	require.NoError(t, err)
	return link, invertPbIssueLink(link)
}

func (suite *RwApiTestSuite) TestGrpcDeleteIssueLink() {
	t := suite.T()
	repoID, _, _ := suite.makeRandomRepo(t, suite.users.Admin)
	client := pb.NewIssueLinkServiceClient(suite.grpcClient)
	adminCtx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)

	// Create issues for link deletion
	issueX := suite.makeIssue(suite.users.Admin, &makeIssueOptions{
		RepoID:     repoID,
		Title:      "Issue X",
		Visibility: entities.IssueVisibilities.Public,
	})
	issueY := suite.makeIssue(suite.users.Admin, &makeIssueOptions{
		RepoID:     repoID,
		Title:      "Issue Y",
		Visibility: entities.IssueVisibilities.Public,
	})
	issueXID := grpc_marshalling.IDInverse(issueX.ID)
	issueYID := grpc_marshalling.IDInverse(issueY.ID)

	createReq := &pb.CreateIssueLinkRequest{
		LeftIssueId:  issueXID,
		RightIssueId: issueYID,
		LinkType:     pb.IssueLink_LINK_TYPE_RELATES,
	}
	resp, err := client.Create(adminCtx, createReq)
	require.NoError(t, err)
	link, err := grpc_marshalling.OperationResponse(resp, &pb.IssueLink{})
	require.NoError(t, err)
	linkID := link.Id

	tt := []struct {
		name           string
		user           *entities.User
		request        *pb.DeleteIssueLinkRequest
		expectedStatus codes.Code
	}{
		{
			name: "no rights to delete",
			user: suite.users.Slowpoke,
			request: &pb.DeleteIssueLinkRequest{
				Id: linkID,
			},
			expectedStatus: codes.PermissionDenied,
		},
		{
			name: "delete existing link",
			user: suite.users.Admin,
			request: &pb.DeleteIssueLinkRequest{
				Id: linkID,
			},
			expectedStatus: codes.OK,
		},
		{
			name: "delete non-existent link",
			user: suite.users.Admin,
			request: &pb.DeleteIssueLinkRequest{
				Id: linkID,
			},
			expectedStatus: codes.NotFound,
		},
		{
			name: "delete non-existent link",
			user: suite.users.Admin,
			request: &pb.DeleteIssueLinkRequest{
				Id: "123456789",
			},
			expectedStatus: codes.NotFound,
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			ctx := testutils.AuthorizeGRPC(tc.user.Identity)
			_, err := client.Delete(ctx, tc.request)
			yarequire.ProtoStatusEqual(t, tc.expectedStatus, err)
			if tc.expectedStatus != codes.OK {
				return
			}

			// Try to get deleted link
			issueLinkID, _ := grpc_marshalling.IDDirect(tc.request.Id)
			_, err = suite.IssueLinkService.GetNormalized(ctx, issueLinkID)
			require.ErrorContains(t, err, except.IssueLinkNotFound.Build(issueLinkID).Error())
		})
	}
}

func invertPbIssueLink(link *pb.IssueLink) *pb.IssueLink {
	invertedLinkType := grpc_marshalling.IssueLinkTypeInverse(grpc_marshalling.IssueLinkTypeDirect(link.LinkType).Inverted())
	return &pb.IssueLink{
		Id:           link.Id,
		LeftIssueId:  link.RightIssueId,
		RightIssueId: link.LeftIssueId,
		LinkType:     invertedLinkType,
		AuthorId:     link.AuthorId,
		UpdatedBy:    link.UpdatedBy,
		CreatedAt:    link.CreatedAt,
		UpdatedAt:    link.UpdatedAt,
	}
}
