package integrationtests

import (
	"bytes"
	"common/testutils/yarequire"
	"fmt"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/httpserver/schemas"
	"gitcore/internal/testutils"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/testing/protocmp"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"testing"
)

func (suite *RwApiTestSuite) TestGrpcMergeRevision() {
	t := suite.T()

	merge02 := suite.ImportRepo(suite.orgs.Yandex, "merge-02", "generated/merge.02", nil)

	tt := map[string]struct {
		request *pb.MergeRevisionsRequest
		code    codes.Code
	}{
		"happy path, no conflict": {
			request: &pb.MergeRevisionsRequest{
				Id:             grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
				SourceRevision: "branch",
				TargetBranch:   "master",
			},
		},
		"simple conflict": {
			request: &pb.MergeRevisionsRequest{
				Id:             grpc_marshalling.IDInverse(merge02.ID),
				SourceRevision: "A",
				TargetBranch:   "B",
			},
		},
	}

	for tn, tc := range tt {
		t.Run(tn, func(t *testing.T) {
			client := pb.NewRepoServiceClient(suite.grpcClient)
			ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)

			repoID, err := grpc_marshalling.IDDirect(tc.request.Id)
			require.NoError(t, err)

			op, err := client.MergeRevisions(ctx, tc.request)
			yarequire.ProtoStatusEqual(t, tc.code, err)
			if tc.code != codes.OK {
				return
			}

			resp, err := grpc_marshalling.OperationResponse(op, &pb.MergeRevisionsResponse{})
			require.NoError(t, err)

			/*
				yarequire.ProtoDumpFixture(t, resp,
					protocmp.IgnoreFields(&pb.MergeRevisionsConflict{}, "id"),
					protocmp.IgnoreFields(&pb.ConflictMessage{}, "id"),
				)
			*/

			yarequire.ProtoCompareWithFixture(t, resp,
				protocmp.IgnoreFields(&pb.MergeRevisionsConflict{}, "id"),
				protocmp.IgnoreFields(&pb.ConflictMessage{}, "id"),
			)

			if resp.GetMergeConflict() == nil {
				targetResp, err := client.ResolveRevision(ctx, &pb.ResolveRevisionRequest{
					Id:       tc.request.Id,
					Revision: tc.request.TargetBranch,
				})
				require.NoError(t, err)

				require.Equal(t,
					fmt.Sprintf("Merge '%s' to '%s'", tc.request.SourceRevision, tc.request.TargetBranch),
					targetResp.GetCommit().Message)

			} else {
				t.Run("find by ID", func(t *testing.T) {
					mc, err := client.GetMergeConflict(ctx, &pb.GetMergeConflictRequest{
						Id: tc.request.Id,
						Conflict: &pb.GetMergeConflictRequest_ConflictId{
							ConflictId: resp.GetMergeConflict().Id,
						},
					})
					require.NoError(t, err)

					/*
						yarequire.ProtoDumpFixture(t, mc,
							protocmp.IgnoreFields(&pb.MergeRevisionsConflict{}, "id"),
							protocmp.IgnoreFields(&pb.ConflictMessage{}, "id"),
						)
					*/

					yarequire.ProtoCompareWithFixture(t, mc,
						protocmp.IgnoreFields(&pb.MergeRevisionsConflict{}, "id"),
						protocmp.IgnoreFields(&pb.ConflictMessage{}, "id"),
					)
				})

				t.Run("find by hashes", func(t *testing.T) {
					mc, err := client.GetMergeConflict(ctx, &pb.GetMergeConflictRequest{
						Id: tc.request.Id,
						Conflict: &pb.GetMergeConflictRequest_Hashes{
							Hashes: &pb.SourceAndTargetHash{
								SourceHash: resp.GetMergeConflict().SourceHash,
								TargetHash: resp.GetMergeConflict().TargetHash,
							},
						},
					})
					require.NoError(t, err)
					/*
						yarequire.ProtoDumpFixture(t, mc,
							protocmp.IgnoreFields(&pb.MergeRevisionsConflict{}, "id"),
							protocmp.IgnoreFields(&pb.ConflictMessage{}, "id"),
						)
					*/
					yarequire.ProtoCompareWithFixture(t, mc,
						protocmp.IgnoreFields(&pb.MergeRevisionsConflict{}, "id"),
						protocmp.IgnoreFields(&pb.ConflictMessage{}, "id"),
					)
				})
				t.Run("resolve", func(t *testing.T) {
					var content = []byte("1234567890")

					key, err := suite.UserUploadService.Upload(ctx, "1.txt",
						schemas.FileUploadTypes.Text,
						int64(len(content)),
						bytes.NewReader(content),
						suite.users.Admin.ID)
					require.NoError(t, err)

					getContent := func(hash plumbing.Hash) []byte {
						gitFS, closer, err := suite.GitFSFactory.Build(repoID).Load(ctx)
						require.NoError(t, err)
						defer func() {
							require.NoError(t, closer())
						}()

						fc, err := gitFS.GetFileContentFromCommitHash(ctx,
							hash,
							resp.GetMergeConflict().Conflicts[1].Paths[0])
						require.NoError(t, err)

						return fc
					}

					for _, tt := range []struct {
						name               string
						resolution         *pb.MergeConflictResolution
						expectedResolution []byte
						fileEdit           *pb.FileEdit
						expectedFileEdit   []byte
					}{
						{
							name: "file_content",
							resolution: &pb.MergeConflictResolution{
								ConflictMessageId: resp.GetMergeConflict().Conflicts[1].Id,
								Resolution: &pb.MergeConflictResolution_UploadKey{
									UploadKey: key,
								},
								Resolved: true,
							},
							expectedResolution: content,
						},
						{
							name: "file_hash_source",
							resolution: &pb.MergeConflictResolution{
								ConflictMessageId: resp.GetMergeConflict().Conflicts[1].Id,
								Resolution: &pb.MergeConflictResolution_PathAndHash{
									PathAndHash: &pb.MergeConflictResolutionPathAndHash{
										Path:       resp.GetMergeConflict().Conflicts[1].Paths[0],
										CommitHash: resp.GetMergeConflict().SourceHash,
									},
								},
								Resolved: true,
							},
							expectedResolution: getContent(plumbing.NewHash(resp.GetMergeConflict().SourceHash)),
						},
						{
							name: "file_hash_target",
							resolution: &pb.MergeConflictResolution{
								ConflictMessageId: resp.GetMergeConflict().Conflicts[1].Id,
								Resolution: &pb.MergeConflictResolution_PathAndHash{
									PathAndHash: &pb.MergeConflictResolutionPathAndHash{
										Path:       resp.GetMergeConflict().Conflicts[1].Paths[0],
										CommitHash: resp.GetMergeConflict().TargetHash,
									},
								},
								Resolved: true,
							},
							expectedResolution: getContent(plumbing.NewHash(resp.GetMergeConflict().TargetHash)),
						},
						{
							name: "file_hash_target_and_additional_file",
							resolution: &pb.MergeConflictResolution{
								ConflictMessageId: resp.GetMergeConflict().Conflicts[1].Id,
								Resolution: &pb.MergeConflictResolution_PathAndHash{
									PathAndHash: &pb.MergeConflictResolutionPathAndHash{
										Path:       resp.GetMergeConflict().Conflicts[1].Paths[0],
										CommitHash: resp.GetMergeConflict().TargetHash,
									},
								},
								Resolved: true,
							},
							expectedResolution: getContent(plumbing.NewHash(resp.GetMergeConflict().TargetHash)),
							fileEdit: &pb.FileEdit{
								Path: "README.NEVER.md",
								Resolution: &pb.FileEdit_PathAndHash{
									PathAndHash: &pb.MergeConflictResolutionPathAndHash{
										Path:       resp.GetMergeConflict().Conflicts[1].Paths[0],
										CommitHash: resp.GetMergeConflict().TargetHash,
									},
								},
							},
							expectedFileEdit: getContent(plumbing.NewHash(resp.GetMergeConflict().TargetHash)),
						},
						{
							name: "file_upload_only",
							fileEdit: &pb.FileEdit{
								Path: "README.NEVER2.md",
								Resolution: &pb.FileEdit_UploadKey{
									UploadKey: key,
								},
							},
							expectedFileEdit: content,
						},
						{
							name: "file_upload_only_change_two_to_one_a",
							fileEdit: &pb.FileEdit{
								Path: "two_to_one_a",
								Resolution: &pb.FileEdit_UploadKey{
									UploadKey: key,
								},
							},
							expectedFileEdit: content,
						},
						{
							name: "file_upload_only_change_two_to_one_b",
							fileEdit: &pb.FileEdit{
								Path: "two_to_one_b",
								Resolution: &pb.FileEdit_UploadKey{
									UploadKey: key,
								},
							},
							expectedFileEdit: content,
						},
					} {
						t.Run(tt.name, func(t *testing.T) {
							var resolutions []*pb.MergeConflictResolution

							if tt.resolution != nil {
								resolutions = []*pb.MergeConflictResolution{
									tt.resolution,
								}
							}

							var fileEdits []*pb.FileEdit
							if tt.fileEdit != nil {
								fileEdits = []*pb.FileEdit{tt.fileEdit}
							}

							resolvedOp, err := client.ResolveConflicts(ctx, &pb.ResolveConflictsRequest{
								Id:          tc.request.Id,
								ConflictId:  resp.GetMergeConflict().Id,
								Resolutions: resolutions,
								FileEdits:   fileEdits,
							})
							require.NoError(t, err)

							resolvedResp, err := grpc_marshalling.OperationResponse(resolvedOp, &pb.ResolveConflictsResponse{})
							require.NoError(t, err)

							mc, err := client.GetMergeConflict(ctx, &pb.GetMergeConflictRequest{
								Id: tc.request.Id,
								Conflict: &pb.GetMergeConflictRequest_ConflictId{
									ConflictId: resp.GetMergeConflict().Id,
								},
							})
							require.NoError(t, err)
							require.Equal(t, resolvedResp.MergeCommitHash, mc.MergeConflict.MergeCommitHash)

							/*
								yarequire.ProtoDumpFixture(t, mc,
									protocmp.IgnoreFields(&pb.MergeRevisionsConflict{}, "id", "merge_commit_hash"),
									protocmp.IgnoreFields(&pb.ConflictMessage{}, "id"),
								)
							*/
							yarequire.ProtoCompareWithFixture(t, mc,
								protocmp.IgnoreFields(&pb.MergeRevisionsConflict{}, "id", "merge_commit_hash"),
								protocmp.IgnoreFields(&pb.ConflictMessage{}, "id"),
							)

							gitFS, closer, err := suite.GitFSFactory.Build(repoID).Load(ctx)
							require.NoError(t, err)
							defer func() {
								require.NoError(t, closer())
							}()

							if tt.resolution != nil {
								fc, err := gitFS.GetFileContentFromCommitHash(ctx,
									plumbing.NewHash(mc.MergeConflict.MergeCommitHash),
									resp.GetMergeConflict().Conflicts[1].Paths[0])
								require.NoError(t, err)
								require.Equal(t, tt.expectedResolution, fc)
							}

							if tt.fileEdit != nil {
								fc, err := gitFS.GetFileContentFromCommitHash(ctx,
									plumbing.NewHash(mc.MergeConflict.MergeCommitHash),
									tt.fileEdit.Path)
								require.NoError(t, err)
								require.Equal(t, tt.expectedFileEdit, fc)
							}
						})
					}
				})
				t.Run("rollback", func(t *testing.T) {
					commitOp, err := client.RollbackResolvedConflicts(ctx, &pb.RollbackResolvedConflictsRequest{
						Id:         tc.request.Id,
						ConflictId: resp.GetMergeConflict().Id,
					})
					require.NoError(t, err)

					_, err = grpc_marshalling.OperationResponse(commitOp, &pb.RollbackResolvedConflictsResponse{})
					require.NoError(t, err)

					resp, err := client.GetMergeConflict(ctx, &pb.GetMergeConflictRequest{
						Id: tc.request.Id,
						Conflict: &pb.GetMergeConflictRequest_ConflictId{
							ConflictId: resp.GetMergeConflict().Id,
						},
					})
					require.NoError(t, err)

					/*
						yarequire.ProtoDumpFixture(t, resp.MergeConflict,
							protocmp.IgnoreFields(&pb.MergeRevisionsConflict{}, "id", "merge_commit_hash"),
							protocmp.IgnoreFields(&pb.ConflictMessage{}, "id"),
						)
					*/
					yarequire.ProtoCompareWithFixture(t, resp.MergeConflict,
						protocmp.IgnoreFields(&pb.MergeRevisionsConflict{}, "id", "merge_commit_hash"),
						protocmp.IgnoreFields(&pb.ConflictMessage{}, "id"),
					)

					resCommitHash := plumbing.NewHash(resp.MergeConflict.MergeCommitHash)

					gitFS, closer, err := suite.GitFSFactory.Build(repoID).Load(ctx)
					require.NoError(t, err)
					defer func() {
						require.NoError(t, closer())
					}()

					cmt, err := gitFS.GetCommitByHash(ctx, resCommitHash)
					require.NoError(t, err)

					_ = cmt
				})
				t.Run("commit", func(t *testing.T) {
					resolvedOp, err := client.ResolveConflicts(ctx, &pb.ResolveConflictsRequest{
						Id:         tc.request.Id,
						ConflictId: resp.GetMergeConflict().Id,
						Resolutions: []*pb.MergeConflictResolution{
							{
								ConflictMessageId: resp.GetMergeConflict().Conflicts[1].Id,
								Resolution: &pb.MergeConflictResolution_PathAndHash{
									PathAndHash: &pb.MergeConflictResolutionPathAndHash{
										Path:       resp.GetMergeConflict().Conflicts[1].Paths[0],
										CommitHash: resp.GetMergeConflict().SourceHash,
									},
								},
								Resolved: true,
							},
						},
					})
					require.NoError(t, err)

					_, err = grpc_marshalling.OperationResponse(resolvedOp, &pb.ResolveConflictsResponse{})
					require.NoError(t, err)

					commitOp, err := client.CommitResolvedConflicts(ctx, &pb.CommitResolvedConflictsRequest{
						Id:         tc.request.Id,
						ConflictId: resp.GetMergeConflict().Id,
					})
					require.NoError(t, err)

					commitRes, err := grpc_marshalling.OperationResponse(commitOp, &pb.CommitResolvedConflictsResponse{})
					require.NoError(t, err)

					resCommitHash := plumbing.NewHash(commitRes.MergeCommitHash)

					gitFS, closer, err := suite.GitFSFactory.Build(repoID).Load(ctx)
					require.NoError(t, err)
					defer func() {
						require.NoError(t, closer())
					}()

					cmt, err := gitFS.GetCommitByHash(ctx, resCommitHash)
					require.NoError(t, err)

					require.Equal(t, 2, cmt.NumParents())
					require.Equal(t, "Merge 'A' to 'B'", cmt.Message)
					require.Equal(t, cmt.ParentHashes[0].String(), resp.GetMergeConflict().TargetHash)
					require.Equal(t, cmt.ParentHashes[1].String(), resp.GetMergeConflict().SourceHash)
				})
			}
		})
	}
}
