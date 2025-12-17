package integrationtests

import (
	"fmt"
	"gitcore/internal/adapters/postgres"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/revision"
	"gitcore/internal/testutils"
	"github.com/Masterminds/squirrel"
	"github.com/stretchr/testify/require"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"reflect"
	"testing"
)

func (suite *RwApiTestSuite) TestRevisioner() {
	suite.T().Run("PR Comments", func(t *testing.T) {
		client := pb.NewPRCommentServiceClient(suite.grpcClient)
		ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)

		pr := suite.makePullRequest(suite.users.Kopatych, nil)
		defaultComment := &makePrCommentOptions{}
		suite.makePrComments(t, suite.users.Kopatych, pr, []*makePrCommentOptions{defaultComment})

		comments, err := client.List(ctx, &pb.ListCommentsRequest{
			PrId: grpc_marshalling.IDInverse(pr.ID),
		})
		require.NoError(t, err)
		require.Equal(t, "2", comments.Revision.Value)
		require.NotNil(t, comments.Revision.Count)
		require.EqualValues(t, 1, *comments.Revision.Count)

		suite.makePrComments(t, suite.users.Kopatych, pr, []*makePrCommentOptions{defaultComment, defaultComment})

		comments, err = client.List(ctx, &pb.ListCommentsRequest{
			PrId: grpc_marshalling.IDInverse(pr.ID),
		})
		require.NoError(t, err)
		require.Equal(t, "4", comments.Revision.Value)
		require.NotNil(t, comments.Revision.Count)
		require.EqualValues(t, 3, *comments.Revision.Count)
	})
}

// Test for developers to not forgot correctly add new revisions
func (suite *RepoApiTestSuite) TestRevisionCorrectness() {
	t := suite.T()
	subtypesMap := suite.value2FieldName(revision.EntitySubtypes)
	typesMap := suite.value2FieldName(revision.EntityTypes)
	revsyncerMapping := postgres.TestOnlyMethodReturnSelectMap()
	suite.checkRevisionedType(revision.PR(0), subtypesMap, typesMap, revsyncerMapping)
	suite.checkRevisionedType(revision.PRComment(0), subtypesMap, typesMap, revsyncerMapping)
	suite.checkRevisionedType(revision.IssueComment(0), subtypesMap, typesMap, revsyncerMapping)
	suite.checkRevisionedType(revision.Issue(0), subtypesMap, typesMap, revsyncerMapping)
	suite.checkRevisionedType(revision.Label(0), subtypesMap, typesMap, revsyncerMapping)
	suite.checkRevisionedType(revision.Milestone(0), subtypesMap, typesMap, revsyncerMapping)
	suite.checkRevisionedType(revision.User(0), subtypesMap, typesMap, revsyncerMapping)
	suite.checkRevisionedType(revision.Repo(0), subtypesMap, typesMap, revsyncerMapping)
	suite.checkRevisionedType(revision.Org(0), subtypesMap, typesMap, revsyncerMapping)
	suite.checkRevisionedType(revision.Achievement(0), subtypesMap, typesMap, revsyncerMapping)
	require.Len(t, typesMap, 10, "You need add checkRevisionedType() for your new revision.EntityType")
}

func (suite *RepoApiTestSuite) checkRevisionedType(
	revisioned revision.Revisioned,
	subtypesMap map[string]string,
	typesMap map[string]string,
	revsyncerMapping map[revision.EntityType]map[revision.EntitySubtype]func(id string) squirrel.SelectBuilder,
) {
	t := suite.T()

	eType := reflect.TypeOf(revisioned)
	eValue := reflect.ValueOf(revisioned)
	allM := eValue.MethodByName("All")
	require.True(t, allM.IsValid())

	allRes := allM.Call([]reflect.Value{})

	allValues := allRes[0]

	allResSubtypes := map[string]struct{}{}
	for i := 0; i < allValues.Len(); i++ {
		v := allValues.Index(i)
		// get EntitySubtype field from revision.Definition
		subtype := v.FieldByName(reflect.TypeOf(revision.EntitySubtype("")).Name())
		subtypeType := subtypesMap[subtype.String()]
		eType := v.FieldByName(reflect.TypeOf(revision.EntityType("")).Name())
		eTypeType := typesMap[eType.String()]
		allResSubtypes[subtypeType] = struct{}{}
		if subtypeType != "Self" &&
			revsyncerMapping[revision.EntityType(eType.String())][revision.EntitySubtype(subtype.String())] == nil {
			require.Fail(t, fmt.Sprintf("no revision syncer for %v %v", eTypeType, subtypeType))
		}
	}
	for i := 0; i < eType.NumField(); i++ {
		field := eType.Field(i)
		if _, ok := allResSubtypes[field.Name]; !ok {
			require.Fail(t, fmt.Sprintf("%v.All() doesn't contain field %v", eType, field.Name))
		}
	}
}

func (suite *RepoApiTestSuite) value2FieldName(t any) map[string]string {
	typeValues := make(map[string]string)
	sTypes := reflect.TypeOf(t)
	sValue := reflect.ValueOf(t)
	for i := 0; i < sTypes.NumField(); i++ {
		field := sTypes.Field(i)
		value := sValue.Field(i)
		strValue := fmt.Sprintf("%v", value.Interface())
		typeValues[strValue] = field.Name
	}
	return typeValues
}
