package integrationtests

import (
	"common/functools"
	"context"
	"fmt"
	"gitcore/internal/di"
	"gitcore/internal/entities"
	except "gitcore/internal/exceptions"
	"gitcore/internal/interfaces"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"
	"testing"
	"time"
)

func TestMergeBases(t *testing.T) {
	for _, avoidGennum := range []bool{false, true} {
		for _, equalDate := range []bool{false, true} {
			t.Run(fmt.Sprintf("nogennum=%v samedate=%v", avoidGennum, equalDate), func(t *testing.T) {
				ctx := context.Background()
				dc := di.WireUp(t,
					fx.Decorate(func(cgf interfaces.CommitGraphFactory) interfaces.CommitGraphFactory {
						return commitGraphFactoryWrapper{
							cgf, avoidGennum, equalDate,
						}
					}),
				)
				dc.World.MakeOrgs(t)

				type testcase struct {
					target, source     string
					bases              []string
					expectedErr        error
					unrelated          bool
					ahead              uint32
					behind             uint32
					disableForSameDate bool
				}

				for _, testRepo := range []struct {
					name      string
					testcases []testcase
				}{
					{
						/*
								Commit graph visualisation:

							* c6f30d2 (HEAD -> A2, A3)
							* e0ebaf9 (A)
							* 32bebe5
							| * 91d0d89 (B2)
							| * 5068fd3 (B)
							| * 64e2755
							| * cb4f651
							|/
							* 11715be (main) 1

						*/
						name: "generated/mergebase",
						testcases: []testcase{
							{
								target: "B2",
								source: "A2",
								bases: []string{
									"11715be9e873fd5ed8f293f1a39e01251df71770",
								},
								ahead:  3,
								behind: 4,
							},
							{
								target: "B2",
								source: "B",
								bases: []string{
									"5068fd3a0b255258d701520974629f881d4309f6",
								},
								ahead:  0,
								behind: 1,
							},
							{
								target: "B",
								source: "A",
								bases: []string{
									"11715be9e873fd5ed8f293f1a39e01251df71770",
								},
								ahead:  2,
								behind: 3,
							},
							{
								target: "A2",
								source: "A3",
								bases: []string{
									"c6f30d20de0e76cb3a75032cba35a719f0fd704a",
								},
								ahead:  0,
								behind: 0,
							},
						},
					},
					{
						/*
								Commit graph visualisation:

							*   57a417a (HEAD -> F) F
							|\
							* \   620abd5 (D) D
							|\ \
							| | | *   3338e51 (G) G
							| | | |\
							| |_|_|/
							|/| | |
							* | | | 32bebe5 (A) A
							| | | * c25af9f (E) E
							| | |/|
							| | |/
							| |/|
							| * | cf94fe9 (B) B
							|/ /
							* / 11715be (main) 1
							 /
							* 623ed46 (C) C


								must be equal to
								git merge-base --all F G

						*/
						name: "generated/crisscross",
						testcases: []testcase{
							{
								target: "F",
								source: "G",
								bases: []string{
									"32bebe5052e8832b533da76792625c33b22f80c1",
									"cf94fe9efa9f38b4d1d6415e4daf6384ea453bd9",
									"623ed46eb58e200936fca11aaa0e1574b8b09a86",
								},
								ahead:              2,
								behind:             1,
								disableForSameDate: true,
							},
							{
								target: "A",
								source: "B",
								bases: []string{
									"11715be9e873fd5ed8f293f1a39e01251df71770",
								},
								ahead:  1,
								behind: 1,
							},
							{
								target: "D",
								source: "E",
								bases: []string{
									"cf94fe9efa9f38b4d1d6415e4daf6384ea453bd9",
								},
								ahead:  1,
								behind: 1,
							},
							{
								target: "E",
								source: "G",
								bases: []string{
									"c25af9f91553ca58f1991270d7af8222e2615de6",
								},
								ahead:              1,
								behind:             0,
								disableForSameDate: true,
							},
							{
								target:      "C",
								source:      "main",
								expectedErr: except.NoMergeBases,
								bases:       []string{},
								unrelated:   true,
							},
						},
					},
					{
						/*
							*   80a75f9 merge unmerged-source-branch to master
							|\
							| * fd6d69d Add for-merged-pr-with-delete.txt
							|/
							*   7b075b5 merge merged-source-branch to master
							|\
							| * 9f25ea3 Add for-merged-pr.txt
							|/
							* 049b3a9 Initial commit
						*/
						name: "extrememergebase.git",
						testcases: []testcase{
							{
								target: "master",
								source: "base",
								bases: []string{
									"9f25ea331411f2fa216136a246b7558925ce5456",
								},
								ahead:  0,
								behind: 2,
							},
						},
					},
				} {
					repoID, _ := dc.World.ImportFixture(t, testRepo.name)
					revisionRepo := dc.ReferenceRepositoryFactory.Build(repoID)

					gitFS, closer, err := dc.GitFSFactory.Build(repoID).Load(ctx)
					require.NoError(t, err)
					defer func() {
						require.NoError(t, closer())
					}()

					cg := dc.CommitGraphFactory.Build()
					err = cg.Init(ctx, gitFS, false)
					require.NoError(t, err)

					mergeBase := dc.MergeBaseFactory.Build(gitFS, cg)

					for _, c := range testRepo.testcases {
						if c.disableForSameDate && equalDate {
							continue
						}

						t.Run(fmt.Sprintf("%s %s %s", testRepo.name, c.target, c.source), func(t *testing.T) {

							targetBranch, err := revisionRepo.GetBranch(ctx, c.target)
							require.NoError(t, err)

							sourceBranch, err := revisionRepo.GetBranch(ctx, c.source)
							require.NoError(t, err)

							bases, err := mergeBase.FindAll(ctx, targetBranch.Hash(), sourceBranch.Hash())
							require.NoError(t, err)

							baseHashes := functools.Map(bases, func(t entities.MergeBase) string {
								return t.Commit.String()
							})

							require.ElementsMatch(t, c.bases, baseHashes)

							if len(bases) > 0 {
								require.Equal(t, c.behind, bases[0].Behind)
								require.Equal(t, c.ahead, bases[0].Ahead)
							}
						})
					}
				}
			})
		}
	}
}

type commitGraphFactoryWrapper struct {
	interfaces.CommitGraphFactory
	avoidGennum bool
	equalDate   bool
}

func (w commitGraphFactoryWrapper) Build() interfaces.CommitGraph {
	return commitGraphWrapper{
		w.CommitGraphFactory.Build(), w.avoidGennum, w.equalDate,
	}
}

type commitGraphWrapper struct {
	interfaces.CommitGraph
	avoidGennum bool
	equalDate   bool
}

func (c commitGraphWrapper) GetCommitByHash(ctx context.Context, hash plumbing.Hash) (*entities.CommitGraphNode, error) {
	cgn, err := c.CommitGraph.GetCommitByHash(ctx, hash)
	if err != nil {
		return nil, err
	}

	if c.avoidGennum {
		cgn.GenerationNumber = 0
	}

	if c.equalDate {
		cgn.Date = time.Unix(2000000000, 0)
	}

	return cgn, nil
}
