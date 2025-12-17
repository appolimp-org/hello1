package integrationtests

import (
	grpc2 "common/grpc"
	"common/testutils/yarequire"
	"common/utils"
	"common/utils/rolesgenerator/iam"
	"gitcore/internal/entities"
	"gitcore/internal/testutils"
	"github.com/stretchr/testify/require"
	"path"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"testing"
)

func (suite *RwApiTestSuite) TestPRGetFileChanges() {
	t := suite.T()

	repo := suite.repos.Alpha
	author := suite.users.Kopatych
	suite.addRole(t, author, repo, iam.Roles.RepositoriesDeveloper)
	cg, tmpDir := suite.initCGit(author, repo.FullSlug())

	filePath := path.Join(tmpDir, "alpha", "newfile.go")

	commit(t, &cg, filePath, content1)
	cg.Must(t, "push")

	cg.Must(t, "checkout", "-b", "new_branch")
	commit(t, &cg, path.Join(tmpDir, "alpha", "README"), "# README\n")
	commit(t, &cg, filePath, content2)
	cg.Must(t, "push", "-u", "origin", "new_branch")

	TARGET := entities.PullRequestCommentSides.Target
	SOURCE := entities.PullRequestCommentSides.Source

	pr := suite.makePullRequest(author, &makePrOptions{
		Title:   "new PR",
		Source:  "new_branch",
		Publish: utils.PtrFromValue(true),
	})
	iteration1 := pr.Iteration

	readmeAnchor := func(side entities.PullRequestCommentSide) *entities.PullRequestCommentAnchor {
		return &entities.PullRequestCommentAnchor{Path: "README", Side: &side, From: utils.PtrFromValue(1), To: utils.PtrFromValue(1)}
	}
	newfileAnchor := func(side entities.PullRequestCommentSide) *entities.PullRequestCommentAnchor {
		return &entities.PullRequestCommentAnchor{Path: "newfile.go", Side: &side, From: utils.PtrFromValue(8), To: utils.PtrFromValue(9)}
	}

	c1 := suite.makePrCommentGRPC(t, author, pr, &makePrCommentOptions{Body: "c1", Anchor: readmeAnchor(SOURCE)})
	cDraft := suite.makePrCommentGRPC(t, author, pr, &makePrCommentOptions{Body: "cDraft", Draft: true, Anchor: readmeAnchor(SOURCE)})
	c2 := suite.makePrCommentGRPC(t, author, pr, &makePrCommentOptions{Body: "c2", Anchor: newfileAnchor(SOURCE)})
	c3 := suite.makePrCommentGRPC(t, author, pr, &makePrCommentOptions{Body: "c3", Anchor: newfileAnchor(TARGET)})

	commit(t, &cg, filePath, content3)
	cg.Must(t, "push")

	c4 := suite.makePrCommentGRPC(t, author, pr, &makePrCommentOptions{Body: "c4", Anchor: &entities.PullRequestCommentAnchor{
		Path: "newfile.go",
		Side: &SOURCE,
		From: utils.PtrFromValue(5),
		To:   utils.PtrFromValue(6),
	}})

	client := pb.NewPRServiceClient(suite.grpcClient)

	testcases := []struct {
		name           string
		fromIter, iter *string
		paths          []*pb.DiffPath
		requester      entities.UserIdentity
		expRes         []*pb.GetFileChangesResponse_Change
	}{
		{
			name: "default - requester is author",
			paths: []*pb.DiffPath{
				{Target: "", Source: "README"},
				{Target: "newfile.go", Source: "newfile.go"},
				{Target: "no/such/file.go", Source: "no/such/file.go"},
				{Target: "binary.jpg", Source: "binary.jpg"},
			},
			requester: testutils.UserIdentities.Kopatych,
			expRes: []*pb.GetFileChangesResponse_Change{
				{
					Diff: &pb.FileDiff{
						Paths: &pb.DiffPath{Target: "", Source: "README"},
						Hunks: []*pb.Hunk{{Patch: "+# README\n", FromStart: 0, FromCount: 0, ToStart: 1, ToCount: 1}},
						Added: 1, Removed: 0, TargetCnt: 0, SourceCnt: 1, Result: pb.FileDiff_SUCCESS,
					},
					Comments: []*pb.GetFileChangesResponse_Comment{
						{CommentId: grpc2.MarshalID(cDraft.ID), Pos: &pb.DiffPos{From: 1, To: 1, Side: pb.DiffPos_SOURCE}},
						{CommentId: grpc2.MarshalID(c1.ID), Pos: &pb.DiffPos{From: 1, To: 1, Side: pb.DiffPos_SOURCE}},
					},
				},
				{
					Diff: &pb.FileDiff{
						Paths: &pb.DiffPath{Target: "newfile.go", Source: "newfile.go"},
						Hunks: []*pb.Hunk{
							{
								Patch: ` import (
 	"fmt"
+	"math"
+	"strconv"
+	"strings"
 )
 
 func main() {
-	s := "Hello, World!"
+	s := "Hello, John!"
 
-	fmt.Println(s)
-	fmt.Println(s)
-	fmt.Println(s)
+	num := 9
 
-	// TODO: remove
 	return
 }
-
-
-
-
-hehe
-
-
-
-hoho
`,
								FromStart: 1, FromCount: 23, ToStart: 1, ToCount: 14,
							},
						},
						Added: 5, Removed: 14, TargetCnt: 23, SourceCnt: 14, Result: pb.FileDiff_SUCCESS,
					},
					Comments: []*pb.GetFileChangesResponse_Comment{
						{CommentId: grpc2.MarshalID(c4.ID), Pos: &pb.DiffPos{From: 5, To: 6, Side: pb.DiffPos_SOURCE}},
						{CommentId: grpc2.MarshalID(c3.ID), Pos: &pb.DiffPos{From: 8, To: 9, Side: pb.DiffPos_TARGET}},
						{CommentId: grpc2.MarshalID(c2.ID), Pos: &pb.DiffPos{From: 10, To: 11, Side: pb.DiffPos_SOURCE}},
					},
				},
				{Diff: &pb.FileDiff{
					Paths:  &pb.DiffPath{Target: "no/such/file.go", Source: "no/such/file.go"},
					Result: pb.FileDiff_FAILURE_NOT_FOUND,
				}},
				{Diff: &pb.FileDiff{
					Paths:  &pb.DiffPath{Target: "binary.jpg", Source: "binary.jpg"},
					Result: pb.FileDiff_FAILURE_BINARY,
				}},
			},
		},
		{
			name: "default - requester is not author",
			paths: []*pb.DiffPath{
				{Target: "", Source: "README"},
				{Target: "newfile.go", Source: "newfile.go"},
				{Target: "no/such/file.go", Source: "no/such/file.go"},
				{Target: "binary.jpg", Source: "binary.jpg"},
			},
			requester: testutils.UserIdentities.Krosh,
			expRes: []*pb.GetFileChangesResponse_Change{
				{
					Diff: &pb.FileDiff{
						Paths: &pb.DiffPath{Target: "", Source: "README"},
						Hunks: []*pb.Hunk{{Patch: "+# README\n", FromStart: 0, FromCount: 0, ToStart: 1, ToCount: 1}},
						Added: 1, Removed: 0, TargetCnt: 0, SourceCnt: 1, Result: pb.FileDiff_SUCCESS,
					},
					Comments: []*pb.GetFileChangesResponse_Comment{
						{
							CommentId: grpc2.MarshalID(c1.ID),
							Pos:       &pb.DiffPos{From: 1, To: 1, Side: pb.DiffPos_SOURCE},
						},
					},
				},
				{
					Diff: &pb.FileDiff{
						Paths: &pb.DiffPath{Target: "newfile.go", Source: "newfile.go"},
						Hunks: []*pb.Hunk{
							{
								Patch: ` import (
 	"fmt"
+	"math"
+	"strconv"
+	"strings"
 )
 
 func main() {
-	s := "Hello, World!"
+	s := "Hello, John!"
 
-	fmt.Println(s)
-	fmt.Println(s)
-	fmt.Println(s)
+	num := 9
 
-	// TODO: remove
 	return
 }
-
-
-
-
-hehe
-
-
-
-hoho
`,
								FromStart: 1, FromCount: 23, ToStart: 1, ToCount: 14,
							},
						},
						Added: 5, Removed: 14, TargetCnt: 23, SourceCnt: 14, Result: pb.FileDiff_SUCCESS,
					},
					Comments: []*pb.GetFileChangesResponse_Comment{
						{CommentId: grpc2.MarshalID(c4.ID), Pos: &pb.DiffPos{From: 5, To: 6, Side: pb.DiffPos_SOURCE}},
						{CommentId: grpc2.MarshalID(c3.ID), Pos: &pb.DiffPos{From: 8, To: 9, Side: pb.DiffPos_TARGET}},
						{CommentId: grpc2.MarshalID(c2.ID), Pos: &pb.DiffPos{From: 10, To: 11, Side: pb.DiffPos_SOURCE}},
					},
				},
				{Diff: &pb.FileDiff{
					Paths:  &pb.DiffPath{Target: "no/such/file.go", Source: "no/such/file.go"},
					Result: pb.FileDiff_FAILURE_NOT_FOUND,
				}},
				{Diff: &pb.FileDiff{
					Paths:  &pb.DiffPath{Target: "binary.jpg", Source: "binary.jpg"},
					Result: pb.FileDiff_FAILURE_BINARY,
				}},
			},
		},
		{
			name: "older iter",
			paths: []*pb.DiffPath{
				{Target: "newfile.go", Source: "newfile.go"},
			},
			requester: testutils.UserIdentities.Kopatych,
			iter:      utils.PtrFromValue(grpc2.MarshalID(iteration1)),
			expRes: []*pb.GetFileChangesResponse_Change{
				{
					Diff: &pb.FileDiff{
						Paths: &pb.DiffPath{Target: "newfile.go", Source: "newfile.go"},
						Hunks: []*pb.Hunk{
							{
								Patch: ` import (
 	"fmt"
+	"math"
 )
 
 func main() {
-	s := "Hello, World!"
+	s := "Hello, John!"
+
+	num := 9
 
 	fmt.Println(s)
 	fmt.Println(s)
 	fmt.Println(s)
 
+	sqrt := math.sqrt(num)
+	fmt.Println(sqrt)
+
 	// TODO: remove
 	return
 }
`,
								FromStart: 1, FromCount: 14, ToStart: 1, ToCount: 20,
							},
						},
						Added: 7, Removed: 1, TargetCnt: 23, SourceCnt: 29, Result: pb.FileDiff_SUCCESS,
					},
					Comments: []*pb.GetFileChangesResponse_Comment{
						{CommentId: grpc2.MarshalID(c3.ID), Pos: &pb.DiffPos{From: 8, To: 9, Side: pb.DiffPos_TARGET}},
						{CommentId: grpc2.MarshalID(c2.ID), Pos: &pb.DiffPos{From: 8, To: 9, Side: pb.DiffPos_SOURCE}},
					},
				},
			},
		},
		{
			name: "between iters",
			paths: []*pb.DiffPath{
				{Target: "newfile.go", Source: "newfile.go"},
			},
			requester: testutils.UserIdentities.Kopatych,
			fromIter:  utils.PtrFromValue(grpc2.MarshalID(iteration1)),
			expRes: []*pb.GetFileChangesResponse_Change{
				{
					Diff: &pb.FileDiff{
						Paths: &pb.DiffPath{Target: "newfile.go", Source: "newfile.go"},
						Hunks: []*pb.Hunk{
							{
								Patch: ` import (
 	"fmt"
 	"math"
+	"strconv"
+	"strings"
 )
 
 func main() {
`,
								FromStart: 1, FromCount: 6, ToStart: 1, ToCount: 8,
							},
							{
								Patch: ` 
 	num := 9
 
-	fmt.Println(s)
-	fmt.Println(s)
-	fmt.Println(s)
-
-	sqrt := math.sqrt(num)
-	fmt.Println(sqrt)
-
-	// TODO: remove
 	return
 }
-
-
-
-
-hehe
-
-
-
-hoho
`,
								FromStart: 8, FromCount: 22, ToStart: 10, ToCount: 5,
							},
						},
						Added: 2, Removed: 17, TargetCnt: 29, SourceCnt: 14, Result: pb.FileDiff_SUCCESS,
					},
					Comments: []*pb.GetFileChangesResponse_Comment{
						{CommentId: grpc2.MarshalID(c4.ID), Pos: &pb.DiffPos{From: 5, To: 6, Side: pb.DiffPos_SOURCE}},
						{CommentId: grpc2.MarshalID(c3.ID), Pos: &pb.DiffPos{From: 11, To: 12, Side: pb.DiffPos_TARGET}},
						{CommentId: grpc2.MarshalID(c2.ID), Pos: &pb.DiffPos{From: 8, To: 9, Side: pb.DiffPos_TARGET}},
					},
				},
			},
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := client.GetFileChanges(testutils.AuthorizeGRPC(tc.requester),
				&pb.GetFileChangesRequest{
					PrId: grpc2.MarshalID(pr.ID), Paths: tc.paths, IterId: tc.iter, FromIterId: tc.fromIter,
				})
			require.NoError(t, err)
			yarequire.ProtoEqualList(t, tc.expRes, res.Changes)
		})
	}
}
