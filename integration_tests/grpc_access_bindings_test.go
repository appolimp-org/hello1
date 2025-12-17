package integrationtests

import (
	"common/testutils/yarequire"
	yautils "common/utils"
	"common/utils/rolesgenerator/iam"
	"gitcore/internal/entities"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/httpserver/httperrors"
	"gitcore/internal/httpserver/schemas"
	"gitcore/internal/testutils"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"slices"
	"strings"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
)

func (suite *RwApiTestSuite) TestAccessBindingsService_OrgBindings() {
	t := suite.T()

	ctx := testutils.AuthorizeGRPC(suite.users.Slowpoke.Identity)
	c := pb.NewAccessBindingsServiceClient(suite.grpcClient)
	orgClient := pb.NewOrgServiceClient(suite.grpcClient)

	// Create organization
	r, err := orgClient.CreateOrg(ctx, &pb.CreateOrgRequest{
		Slug:        yautils.MustMakeRandomString("slug", 16),
		Description: "Yandex",
		DisplayName: "Yandex",
		Visibility:  pb.ProfileVisibility_PROFILE_PUBLIC,
	})
	require.NoError(t, err)

	var profile pb.OrgProfile
	require.NoError(t, r.GetResponse().UnmarshalTo(&profile))

	orgID := profile.Id

	obj := &pb.Object{Identifier: &pb.Object_OrgId{OrgId: orgID}}

	// List
	listRes, err := c.ListAccessBindings(ctx, &pb.ListAccessBindingsRequest{
		Object: obj,
	})
	require.NoError(t, err)

	require.Len(t, listRes.Bindings, 1)
	require.Equal(t, string(iam.Roles.OrganizationManagerOrganizationsOwner), listRes.Bindings[0].Role)
	require.Equal(t, pb.Subject_USER, listRes.Bindings[0].Subject.Type)
	require.Equal(t, grpc_marshalling.IDInverse(suite.users.Slowpoke.ID), listRes.Bindings[0].Subject.Id)

	// Create
	deltas := []*pb.AccessBindingDelta{
		{
			Action: pb.DeltaAction_ADD,
			Binding: &pb.AccessBinding{
				Role: string(iam.Roles.RepositoriesMaintainer),
				Subject: &pb.Subject{
					Type: pb.Subject_USER,
					Id:   grpc_marshalling.IDInverse(suite.users.Slowpoke.ID),
				},
			},
		},
	}
	_, err = c.UpdateAccessBindings(ctx, &pb.UpdateAccessBindingsRequest{
		Object:        obj,
		BindingDeltas: deltas,
	})
	require.NoError(t, err)

	// List
	listRes, err = c.ListAccessBindings(ctx, &pb.ListAccessBindingsRequest{
		Object: obj,
	})
	require.NoError(t, err)

	require.Len(t, listRes.Bindings, 2)
	maintainerIdx := slices.IndexFunc(listRes.Bindings, func(binding *pb.AccessBinding) bool {
		return binding.Role == string(iam.Roles.RepositoriesMaintainer)
	})

	require.Equal(t, pb.Subject_USER, listRes.Bindings[maintainerIdx].Subject.Type)
	require.Equal(t, grpc_marshalling.IDInverse(suite.users.Slowpoke.ID), listRes.Bindings[maintainerIdx].Subject.Id)

	// List from other user
	listRes, err = c.ListAccessBindings(
		testutils.AuthorizeGRPC(suite.users.Raichu.Identity),
		&pb.ListAccessBindingsRequest{
			Object: obj,
		})
	yarequire.ProtoStatusEqual(t, codes.PermissionDenied, err)
	require.Nil(t, listRes)

	// Remove
	deltas = []*pb.AccessBindingDelta{
		{
			Action: pb.DeltaAction_REMOVE,
			Binding: &pb.AccessBinding{
				Role: string(iam.Roles.RepositoriesMaintainer),
				Subject: &pb.Subject{
					Type: pb.Subject_USER,
					Id:   grpc_marshalling.IDInverse(suite.users.Slowpoke.ID),
				},
			},
		},
	}
	_, err = c.UpdateAccessBindings(ctx, &pb.UpdateAccessBindingsRequest{
		Object:        obj,
		BindingDeltas: deltas,
	})
	require.NoError(t, err)

	// List
	listRes, err = c.ListAccessBindings(ctx, &pb.ListAccessBindingsRequest{
		Object: obj,
	})
	require.NoError(t, err)

	require.Len(t, listRes.Bindings, 1)
	require.Equal(t, string(iam.Roles.OrganizationManagerOrganizationsOwner), listRes.Bindings[0].Role)

	// Remove non-existent should not return 5xx
	_, err = c.UpdateAccessBindings(ctx, &pb.UpdateAccessBindingsRequest{
		Object:        obj,
		BindingDeltas: deltas,
	})
	require.NoError(t, err)
}

func (suite *RwApiTestSuite) TestAccessBindingsService_RepoBindings() {
	t := suite.T()

	ctx := testutils.AuthorizeGRPC(suite.users.Slowpoke.Identity)
	c := pb.NewAccessBindingsServiceClient(suite.grpcClient)
	orgClient := pb.NewOrgServiceClient(suite.grpcClient)

	// Create organization
	r, err := orgClient.CreateOrg(ctx, &pb.CreateOrgRequest{
		Slug:        yautils.MustMakeRandomString("slug", 16),
		Description: "Yandex",
		DisplayName: "Yandex",
		Visibility:  pb.ProfileVisibility_PROFILE_PUBLIC,
	})
	require.NoError(t, err)

	var profile pb.OrgProfile
	require.NoError(t, r.GetResponse().UnmarshalTo(&profile))

	orgSlug := profile.Slug

	// Create repository
	httpErr := httperrors.APIError{}
	repoDetails := &schemas.RepoDetails{}

	testutils.Expect(suite.client.As(testutils.UserIdentities.Slowpoke).
		SetError(&httpErr).
		SetResult(repoDetails).
		SetBody(&schemas.CreateRepositoryRequest{
			Name:    "repo",
			Slug:    "repo",
			OrgSlug: &orgSlug,
		}).
		Post("/api/v1/repos")).MustBe(t, 201)

	repoID, err := repoDetails.ID.ToUint64()
	require.NoError(t, err)

	obj := &pb.Object{Identifier: &pb.Object_RepoId{RepoId: grpc_marshalling.IDInverse(repoID)}}

	// List
	listRes, err := c.ListAccessBindings(ctx, &pb.ListAccessBindingsRequest{
		Object: obj,
	})
	require.NoError(t, err)

	require.Len(t, listRes.Bindings, 1)
	require.Equal(t, string(iam.Roles.RepositoriesAdmin), listRes.Bindings[0].Role)
	require.Equal(t, pb.Subject_USER, listRes.Bindings[0].Subject.Type)
	require.Equal(t, grpc_marshalling.IDInverse(suite.users.Slowpoke.ID), listRes.Bindings[0].Subject.Id)

	// Create
	deltas := []*pb.AccessBindingDelta{
		{
			Action: pb.DeltaAction_ADD,
			Binding: &pb.AccessBinding{
				Role: string(iam.Roles.Admin),
				Subject: &pb.Subject{
					Type: pb.Subject_USER,
					Id:   grpc_marshalling.IDInverse(suite.users.Krosh.ID),
				},
			},
		},
	}
	_, err = c.UpdateAccessBindings(ctx, &pb.UpdateAccessBindingsRequest{
		Object:        obj,
		BindingDeltas: deltas,
	})
	require.NoError(t, err)

	// List
	listRes, err = c.ListAccessBindings(ctx, &pb.ListAccessBindingsRequest{
		Object: obj,
	})
	require.NoError(t, err)

	require.Len(t, listRes.Bindings, 2)
	require.Equal(t, string(iam.Roles.Admin), listRes.Bindings[1].Role)
	require.Equal(t, pb.Subject_USER, listRes.Bindings[1].Subject.Type)
	require.Equal(t, grpc_marshalling.IDInverse(suite.users.Krosh.ID), listRes.Bindings[1].Subject.Id)

	// List from other user
	listRes, err = c.ListAccessBindings(
		testutils.AuthorizeGRPC(suite.users.Raichu.Identity),
		&pb.ListAccessBindingsRequest{
			Object: obj,
		})
	yarequire.ProtoStatusEqual(t, codes.PermissionDenied, err)
	require.Nil(t, listRes)

	// Remove
	deltas = []*pb.AccessBindingDelta{
		{
			Action: pb.DeltaAction_REMOVE,
			Binding: &pb.AccessBinding{
				Role: string(iam.Roles.Admin),
				Subject: &pb.Subject{
					Type: pb.Subject_USER,
					Id:   grpc_marshalling.IDInverse(suite.users.Krosh.ID),
				},
			},
		},
	}
	_, err = c.UpdateAccessBindings(ctx, &pb.UpdateAccessBindingsRequest{
		Object:        obj,
		BindingDeltas: deltas,
	})
	require.NoError(t, err)

	// List
	listRes, err = c.ListAccessBindings(ctx, &pb.ListAccessBindingsRequest{
		Object: obj,
	})
	require.NoError(t, err)

	require.Len(t, listRes.Bindings, 1)
	require.Equal(t, string(iam.Roles.RepositoriesAdmin), listRes.Bindings[0].Role)
	require.Equal(t, pb.Subject_USER, listRes.Bindings[0].Subject.Type)
	require.Equal(t, grpc_marshalling.IDInverse(suite.users.Slowpoke.ID), listRes.Bindings[0].Subject.Id)
}

func (suite *RwApiTestSuite) TestAccessBindingsService_ProjBindings() {
	t := suite.T()

	ctx := testutils.AuthorizeGRPC(suite.users.Slowpoke.Identity)
	c := pb.NewAccessBindingsServiceClient(suite.grpcClient)
	orgClient := pb.NewOrgServiceClient(suite.grpcClient)

	// Create organization
	r, err := orgClient.CreateOrg(ctx, &pb.CreateOrgRequest{
		Slug:        yautils.MustMakeRandomString("slug", 16),
		Description: "Yandex",
		DisplayName: "Yandex",
		Visibility:  pb.ProfileVisibility_PROFILE_PUBLIC,
	})
	require.NoError(t, err)

	var profile pb.OrgProfile
	require.NoError(t, r.GetResponse().UnmarshalTo(&profile))

	orgSlug := profile.Slug

	// Create project
	orgID, err := grpc_marshalling.IDDirect(profile.Id)
	require.NoError(t, err)
	projDetails := entities.Project{
		Name:       "project",
		Slug:       "prj",
		OrgID:      orgID,
		Visibility: entities.Visibilities.Public,
	}
	_, err = suite.OrgRepo.CreateProject(ctx, &projDetails)
	require.NoError(t, err)

	proj, err := suite.OrgRepo.GetProject(ctx, orgSlug, projDetails.Slug)
	require.NoError(t, err)

	obj := &pb.Object{Identifier: &pb.Object_ProjectId{ProjectId: grpc_marshalling.IDInverse(proj.ID)}}

	// List
	listRes, err := c.ListAccessBindings(ctx, &pb.ListAccessBindingsRequest{
		Object: obj,
	})
	require.NoError(t, err)

	require.Empty(t, listRes.Bindings)

	// Create
	deltas := []*pb.AccessBindingDelta{
		{
			Action: pb.DeltaAction_ADD,
			Binding: &pb.AccessBinding{
				Role: string(iam.Roles.Admin),
				Subject: &pb.Subject{
					Type: pb.Subject_USER,
					Id:   grpc_marshalling.IDInverse(suite.users.Slowpoke.ID),
				},
			},
		},
	}
	_, err = c.UpdateAccessBindings(ctx, &pb.UpdateAccessBindingsRequest{
		Object:        obj,
		BindingDeltas: deltas,
	})
	require.NoError(t, err)

	// List
	listRes, err = c.ListAccessBindings(ctx, &pb.ListAccessBindingsRequest{
		Object: obj,
	})
	require.NoError(t, err)

	require.Len(t, listRes.Bindings, 1)
	require.Equal(t, string(iam.Roles.Admin), listRes.Bindings[0].Role)
	require.Equal(t, pb.Subject_USER, listRes.Bindings[0].Subject.Type)
	require.Equal(t, grpc_marshalling.IDInverse(suite.users.Slowpoke.ID), listRes.Bindings[0].Subject.Id)

	// List from other user
	listRes, err = c.ListAccessBindings(
		testutils.AuthorizeGRPC(suite.users.Raichu.Identity),
		&pb.ListAccessBindingsRequest{
			Object: obj,
		})
	yarequire.ProtoStatusEqual(t, codes.PermissionDenied, err)
	require.Nil(t, listRes)

	// Remove
	deltas = []*pb.AccessBindingDelta{
		{
			Action: pb.DeltaAction_REMOVE,
			Binding: &pb.AccessBinding{
				Role: string(iam.Roles.Admin),
				Subject: &pb.Subject{
					Type: pb.Subject_USER,
					Id:   grpc_marshalling.IDInverse(suite.users.Slowpoke.ID),
				},
			},
		},
	}
	_, err = c.UpdateAccessBindings(ctx, &pb.UpdateAccessBindingsRequest{
		Object:        obj,
		BindingDeltas: deltas,
	})
	require.NoError(t, err)

	// Remove non-existent should not return 5xx
	_, err = c.UpdateAccessBindings(ctx, &pb.UpdateAccessBindingsRequest{
		Object:        obj,
		BindingDeltas: deltas,
	})
	require.NoError(t, err)

	// List
	listRes, err = c.ListAccessBindings(ctx, &pb.ListAccessBindingsRequest{
		Object: obj,
	})
	require.NoError(t, err)

	require.Empty(t, listRes.Bindings)
}

func (suite *RwApiTestSuite) TestAccessBindingsService_SetUpdate() {
	t := suite.T()

	ctx := testutils.AuthorizeGRPC(suite.users.Slowpoke.Identity)
	c := pb.NewAccessBindingsServiceClient(suite.grpcClient)
	orgClient := pb.NewOrgServiceClient(suite.grpcClient)

	// Create organization
	r, err := orgClient.CreateOrg(ctx, &pb.CreateOrgRequest{
		Slug:        yautils.MustMakeRandomString("slug", 16),
		Description: "Yandex",
		DisplayName: "Yandex",
		Visibility:  pb.ProfileVisibility_PROFILE_PUBLIC,
	})
	require.NoError(t, err)

	var profile pb.OrgProfile
	require.NoError(t, r.GetResponse().UnmarshalTo(&profile))

	orgID := profile.Id

	obj := &pb.Object{Identifier: &pb.Object_OrgId{OrgId: orgID}}

	// List
	listRes, err := c.ListAccessBindings(ctx, &pb.ListAccessBindingsRequest{
		Object: obj,
	})
	require.NoError(t, err)

	require.Len(t, listRes.Bindings, 1)
	require.Equal(t, string(iam.Roles.OrganizationManagerOrganizationsOwner), listRes.Bindings[0].Role)
	require.Equal(t, pb.Subject_USER, listRes.Bindings[0].Subject.Type)
	require.Equal(t, grpc_marshalling.IDInverse(suite.users.Slowpoke.ID), listRes.Bindings[0].Subject.Id)

	// Set
	bindings := []*pb.AccessBinding{
		{
			Role: string(iam.Roles.Viewer),
			Subject: &pb.Subject{
				Type: pb.Subject_USER,
				Id:   grpc_marshalling.IDInverse(suite.users.Krosh.ID),
			},
		},
		{
			Role: string(iam.Roles.OrganizationManagerOrganizationsOwner),
			Subject: &pb.Subject{
				Type: pb.Subject_USER,
				Id:   grpc_marshalling.IDInverse(suite.users.Slowpoke.ID),
			},
		},
	}
	op, err := c.SetAccessBindings(ctx, &pb.SetAccessBindingsRequest{
		Object:   obj,
		Bindings: bindings,
	})
	require.NoError(t, err)
	resp := testutils.UnmarshalGrpcResult[*pb.AccessBindingsOperationResult](t, op)

	accessBindingsCmp := func(a, b *pb.AccessBinding) int {
		return strings.Compare(a.Subject.Id, b.Subject.Id)
	}
	slices.SortFunc(resp.Bindings, accessBindingsCmp)
	yarequire.ProtoEqualList(t, bindings, resp.Bindings)

	// List
	listRes, err = c.ListAccessBindings(ctx, &pb.ListAccessBindingsRequest{
		Object: obj,
	})
	require.NoError(t, err)
	slices.SortFunc(listRes.Bindings, accessBindingsCmp)
	yarequire.ProtoEqualList(t, bindings, listRes.Bindings)

	// Update
	deltas := []*pb.AccessBindingDelta{
		{
			Action: pb.DeltaAction_REMOVE,
			Binding: &pb.AccessBinding{
				Role: string(iam.Roles.Viewer),
				Subject: &pb.Subject{
					Type: pb.Subject_USER,
					Id:   grpc_marshalling.IDInverse(suite.users.Krosh.ID),
				},
			},
		},
		{
			Action: pb.DeltaAction_ADD,
			Binding: &pb.AccessBinding{
				Role: string(iam.Roles.Viewer),
				Subject: &pb.Subject{
					Type: pb.Subject_USER,
					Id:   grpc_marshalling.IDInverse(suite.users.Kopatych.ID),
				},
			},
		},
	}
	op, err = c.UpdateAccessBindings(ctx, &pb.UpdateAccessBindingsRequest{
		Object:        obj,
		BindingDeltas: deltas,
	})
	require.NoError(t, err)
	resp = testutils.UnmarshalGrpcResult[*pb.AccessBindingsOperationResult](t, op)
	slices.SortFunc(resp.Bindings, accessBindingsCmp)
	expected := []*pb.AccessBinding{deltas[1].Binding, bindings[1]}
	yarequire.ProtoEqualList(t, expected, resp.Bindings)

	// List
	listRes, err = c.ListAccessBindings(ctx, &pb.ListAccessBindingsRequest{
		Object: obj,
	})
	require.NoError(t, err)
	slices.SortFunc(listRes.Bindings, accessBindingsCmp)
	yarequire.ProtoEqualList(t, expected, listRes.Bindings)
}

func (suite *RwApiTestSuite) TestAccessBindingsService_TeamBindings() {
	t := suite.T()

	ctx := testutils.AuthorizeGRPC(suite.users.Slowpoke.Identity)
	c := pb.NewAccessBindingsServiceClient(suite.grpcClient)
	orgClient := pb.NewOrgServiceClient(suite.grpcClient)
	teamClient := pb.NewTeamServiceClient(suite.grpcClient)

	// Create organization
	r, err := orgClient.CreateOrg(ctx, &pb.CreateOrgRequest{
		Slug:        yautils.MustMakeRandomString("slug", 16),
		Description: "Yandex",
		DisplayName: "Yandex",
		Visibility:  pb.ProfileVisibility_PROFILE_PUBLIC,
	})
	require.NoError(t, err)

	var profile pb.OrgProfile
	require.NoError(t, r.GetResponse().UnmarshalTo(&profile))

	orgID := profile.Id

	obj := &pb.Object{Identifier: &pb.Object_OrgId{OrgId: orgID}}

	// Create team
	op, err := teamClient.Create(ctx, &pb.CreateTeamRequest{
		OrgId: orgID,
		Name:  "test-team",
	})
	require.NoError(t, err)
	var teampb pb.Team
	err = op.GetResponse().UnmarshalTo(&teampb)
	require.NoError(t, err)

	// List
	listRes, err := c.ListAccessBindings(ctx, &pb.ListAccessBindingsRequest{
		Object: obj,
	})
	require.NoError(t, err)
	require.Len(t, listRes.Bindings, 1)
	require.Equal(t, string(iam.Roles.OrganizationManagerOrganizationsOwner), listRes.Bindings[0].Role)
	require.Equal(t, pb.Subject_USER, listRes.Bindings[0].Subject.Type)
	require.Equal(t, grpc_marshalling.IDInverse(suite.users.Slowpoke.ID), listRes.Bindings[0].Subject.Id)

	// Create
	deltas := []*pb.AccessBindingDelta{
		{
			Action: pb.DeltaAction_ADD,
			Binding: &pb.AccessBinding{
				Role: string(iam.Roles.RepositoriesMaintainer),
				Subject: &pb.Subject{
					Type: pb.Subject_TEAM,
					Id:   teampb.Identity.Id,
				},
			},
		},
	}
	_, err = c.UpdateAccessBindings(ctx, &pb.UpdateAccessBindingsRequest{
		Object:        obj,
		BindingDeltas: deltas,
	})
	require.NoError(t, err)

	// List
	listRes, err = c.ListAccessBindings(ctx, &pb.ListAccessBindingsRequest{
		Object: obj,
	})
	require.NoError(t, err)

	require.Len(t, listRes.Bindings, 2)
	maintainerIdx := slices.IndexFunc(listRes.Bindings, func(binding *pb.AccessBinding) bool {
		return binding.Role == string(iam.Roles.RepositoriesMaintainer)
	})

	require.Equal(t, pb.Subject_TEAM, listRes.Bindings[maintainerIdx].Subject.Type)
	require.Equal(t, teampb.Identity.Id, listRes.Bindings[maintainerIdx].Subject.Id)

	// List from other user
	listRes, err = c.ListAccessBindings(
		testutils.AuthorizeGRPC(suite.users.Raichu.Identity),
		&pb.ListAccessBindingsRequest{
			Object: obj,
		})
	yarequire.ProtoStatusEqual(t, codes.PermissionDenied, err)
	require.Nil(t, listRes)

	// Remove
	deltas = []*pb.AccessBindingDelta{
		{
			Action: pb.DeltaAction_REMOVE,
			Binding: &pb.AccessBinding{
				Role: string(iam.Roles.RepositoriesMaintainer),
				Subject: &pb.Subject{
					Type: pb.Subject_TEAM,
					Id:   teampb.Identity.Id,
				},
			},
		},
	}
	_, err = c.UpdateAccessBindings(ctx, &pb.UpdateAccessBindingsRequest{
		Object:        obj,
		BindingDeltas: deltas,
	})
	require.NoError(t, err)

	// List
	listRes, err = c.ListAccessBindings(ctx, &pb.ListAccessBindingsRequest{
		Object: obj,
	})
	require.NoError(t, err)

	require.Len(t, listRes.Bindings, 1)
	require.Equal(t, string(iam.Roles.OrganizationManagerOrganizationsOwner), listRes.Bindings[0].Role)

	// Remove non-existent should not return 5xx
	_, err = c.UpdateAccessBindings(ctx, &pb.UpdateAccessBindingsRequest{
		Object:        obj,
		BindingDeltas: deltas,
	})
	require.NoError(t, err)
}
