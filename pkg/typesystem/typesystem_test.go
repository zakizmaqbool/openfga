package typesystem

import (
	"context"
	"sort"
	"testing"

	parser "github.com/craigpastro/openfga-dsl-parser/v2"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"github.com/stretchr/testify/require"
)

func TestNewAndValidate(t *testing.T) {

	tests := []struct {
		name          string
		model         string
		expectedError error
	}{
		{
			name: "direct_relationship_with_entrypoint",
			model: `
			type user

			type document
			  relations
			    define viewer: [user] as self
			`,
		},
		{
			name: "computed_relationship_with_entrypoint",
			model: `
			type user

			type document
			  relations
			    define editor: [user] as self
			    define viewer as editor
			`,
		},
		{
			name: "no_entrypoint_1",
			model: `
			type user

			type document
			  relations
			    define admin: [user] as self
			    define action1 as admin and action2 and action3
			    define action2 as admin and action1 and action3
			    define action3 as admin and action1 and action2
			`,
			expectedError: ErrNoEntryPointsLoop,
		},
		{
			name: "no_entrypoint_2",
			model: `
			type user

			type document
			  relations
				define admin: [user] as self
				define action1 as admin but not action2
				define action2 as admin but not action3
				define action3 as admin but not action1
			`,
			expectedError: ErrNoEntryPointsLoop,
		},
		{
			name: "no_entrypoint_3a",
			model: `
			type user

			type document
			  relations
			    define viewer: [document#viewer] as self and editor
			    define editor: [user] as self
			`,
			expectedError: ErrNoEntrypoints,
		},
		{
			name: "no_entrypoint_3b",
			model: `
			type user

			type document
			  relations
			    define viewer: [document#viewer] as self but not editor
			    define editor: [user] as self
			`,
			expectedError: ErrNoEntrypoints,
		},
		{
			name: "no_entrypoint_4",
			model: `
			type user

			type folder
			  relations
			    define parent: [document] as self
			    define viewer as editor from parent

			type document
			  relations
			    define parent: [folder] as self
				define editor as viewer
			    define viewer as editor from parent
			`,
			expectedError: ErrNoEntrypoints,
		},
		{
			name: "self_referencing_type_restriction_with_entrypoint_1",
			model: `
			type user

			type document
			  relations
			    define restricted: [user] as self
			    define editor: [user] as self
			    define viewer: [document#viewer] as self or editor
			    define can_view as viewer but not restricted
			    define can_view_actual as can_view
			`,
		},
		{
			name: "self_referencing_type_restriction_with_entrypoint_2",
			model: `
			type user

			type document
			  relations
			    define editor: [user] as self
			    define viewer: [document#viewer] as self or editor
			`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewAndValidate(context.Background(), &openfgav1.AuthorizationModel{
				SchemaVersion:   SchemaVersion1_1,
				TypeDefinitions: parser.MustParse(test.model),
			})
			require.ErrorIs(t, err, test.expectedError)
		})
	}
}

func TestSuccessfulRewriteValidations(t *testing.T) {
	var tests = []struct {
		name  string
		model *openfgav1.AuthorizationModel
	}{
		{
			name: "empty_relations",
			model: &openfgav1.AuthorizationModel{
				SchemaVersion: SchemaVersion1_1,
				TypeDefinitions: []*openfgav1.TypeDefinition{
					{
						Type: "repo",
					},
				},
			},
		},
		{
			name: "zero_length_relations_is_valid",
			model: &openfgav1.AuthorizationModel{
				SchemaVersion: SchemaVersion1_1,
				TypeDefinitions: []*openfgav1.TypeDefinition{
					{
						Type:      "repo",
						Relations: map[string]*openfgav1.Userset{},
					},
				},
			},
		},
		{
			name: "self_referencing_type_restriction_with_entrypoint",
			model: &openfgav1.AuthorizationModel{
				TypeDefinitions: parser.MustParse(`
				type user

				type document
				  relations
				    define editor: [user] as self
				    define viewer: [document#viewer] as self or editor
				`),
				SchemaVersion: SchemaVersion1_1,
			},
		},
		{
			name: "intersection_may_contain_repeated_relations",
			model: &openfgav1.AuthorizationModel{
				TypeDefinitions: parser.MustParse(`
				type user
				type document
				  relations
					define editor: [user] as self
					define viewer as editor and editor
				`),
				SchemaVersion: SchemaVersion1_1,
			},
		},
		{
			name: "exclusion_may_contain_repeated_relations",
			model: &openfgav1.AuthorizationModel{
				TypeDefinitions: parser.MustParse(`
				type user
				type document
				  relations
					define editor: [user] as self
					define viewer as editor but not editor
				`),
				SchemaVersion: SchemaVersion1_1,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewAndValidate(context.Background(), test.model)
			require.NoError(t, err)
		})
	}
}

func TestInvalidRewriteValidations(t *testing.T) {
	var tests = []struct {
		name  string
		model *openfgav1.AuthorizationModel
		err   error
	}{
		{
			name: "empty_rewrites",
			model: &openfgav1.AuthorizationModel{
				SchemaVersion: SchemaVersion1_1,
				TypeDefinitions: []*openfgav1.TypeDefinition{
					{
						Type: "document",
						Relations: map[string]*openfgav1.Userset{
							"reader": {},
						},
					},
				},
			},
			err: ErrInvalidUsersetRewrite,
		},
		{
			name: "duplicate_types_is_invalid",
			model: &openfgav1.AuthorizationModel{
				SchemaVersion: SchemaVersion1_1,
				TypeDefinitions: []*openfgav1.TypeDefinition{
					{
						Type:      "repo",
						Relations: map[string]*openfgav1.Userset{},
					},
					{
						Type:      "repo",
						Relations: map[string]*openfgav1.Userset{},
					},
				},
			},
			err: ErrDuplicateTypes,
		},
		{
			name: "invalid_relation:_self_reference_in_computedUserset",
			model: &openfgav1.AuthorizationModel{
				SchemaVersion: SchemaVersion1_1,
				TypeDefinitions: []*openfgav1.TypeDefinition{
					{
						Type: "document",
						Relations: map[string]*openfgav1.Userset{
							"reader": {
								Userset: &openfgav1.Userset_ComputedUserset{
									ComputedUserset: &openfgav1.ObjectRelation{Relation: "reader"},
								},
							},
						},
					},
				},
			},
			err: ErrInvalidUsersetRewrite,
		},
		{
			name: "invalid_relation:_self_reference_in_union",
			model: &openfgav1.AuthorizationModel{
				SchemaVersion: SchemaVersion1_1,
				TypeDefinitions: []*openfgav1.TypeDefinition{
					{
						Type: "document",
						Relations: map[string]*openfgav1.Userset{
							"reader": {
								Userset: &openfgav1.Userset_Union{
									Union: &openfgav1.Usersets{
										Child: []*openfgav1.Userset{
											{
												Userset: &openfgav1.Userset_This{},
											},
											{
												Userset: &openfgav1.Userset_ComputedUserset{
													ComputedUserset: &openfgav1.ObjectRelation{Relation: "reader"},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			err: ErrInvalidUsersetRewrite,
		},
		{
			name: "invalid_relation:_self_reference_in_intersection",
			model: &openfgav1.AuthorizationModel{
				SchemaVersion: SchemaVersion1_1,
				TypeDefinitions: []*openfgav1.TypeDefinition{
					{
						Type: "document",
						Relations: map[string]*openfgav1.Userset{
							"reader": {
								Userset: &openfgav1.Userset_Intersection{
									Intersection: &openfgav1.Usersets{
										Child: []*openfgav1.Userset{
											{
												Userset: &openfgav1.Userset_This{},
											},
											{
												Userset: &openfgav1.Userset_ComputedUserset{
													ComputedUserset: &openfgav1.ObjectRelation{Relation: "reader"},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			err: ErrInvalidUsersetRewrite,
		},
		{
			name: "invalid_relation:_self_reference_in_difference_base",
			model: &openfgav1.AuthorizationModel{
				SchemaVersion: SchemaVersion1_1,
				TypeDefinitions: []*openfgav1.TypeDefinition{
					{
						Type: "document",
						Relations: map[string]*openfgav1.Userset{
							"reader": {
								Userset: &openfgav1.Userset_Difference{
									Difference: &openfgav1.Difference{
										Base: &openfgav1.Userset{
											Userset: &openfgav1.Userset_ComputedUserset{
												ComputedUserset: &openfgav1.ObjectRelation{Relation: "reader"},
											},
										},
										Subtract: &openfgav1.Userset{
											Userset: &openfgav1.Userset_This{},
										},
									},
								},
							},
						},
					},
				},
			},
			err: ErrInvalidUsersetRewrite,
		},
		{
			name: "invalid_relation:_self_reference_in_difference_subtract",
			model: &openfgav1.AuthorizationModel{
				SchemaVersion: SchemaVersion1_1,
				TypeDefinitions: []*openfgav1.TypeDefinition{
					{
						Type: "document",
						Relations: map[string]*openfgav1.Userset{
							"reader": {
								Userset: &openfgav1.Userset_Difference{
									Difference: &openfgav1.Difference{
										Base: &openfgav1.Userset{
											Userset: &openfgav1.Userset_This{},
										},
										Subtract: &openfgav1.Userset{
											Userset: &openfgav1.Userset_ComputedUserset{
												ComputedUserset: &openfgav1.ObjectRelation{Relation: "reader"},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			err: ErrInvalidUsersetRewrite,
		},
		{
			name: "invalid_relation:_computedUserset_to_relation_which_does_not_exist",
			model: &openfgav1.AuthorizationModel{
				SchemaVersion: SchemaVersion1_1,
				TypeDefinitions: []*openfgav1.TypeDefinition{
					{
						Type: "document",
						Relations: map[string]*openfgav1.Userset{
							"reader": {
								Userset: &openfgav1.Userset_ComputedUserset{
									ComputedUserset: &openfgav1.ObjectRelation{Relation: "writer"},
								},
							},
						},
					},
				},
			},
			err: ErrRelationUndefined,
		},
		{
			name: "invalid_relation:_computedUserset_in_a_union",
			model: &openfgav1.AuthorizationModel{
				SchemaVersion: SchemaVersion1_1,
				TypeDefinitions: []*openfgav1.TypeDefinition{
					{
						Type: "document",
						Relations: map[string]*openfgav1.Userset{
							"reader": {
								Userset: &openfgav1.Userset_Union{
									Union: &openfgav1.Usersets{
										Child: []*openfgav1.Userset{
											{
												Userset: &openfgav1.Userset_This{},
											},
											{
												Userset: &openfgav1.Userset_ComputedUserset{
													ComputedUserset: &openfgav1.ObjectRelation{Relation: "writer"},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			err: ErrRelationUndefined,
		},
		{
			name: "invalid_relation:_computedUserset_in_a_intersection",
			model: &openfgav1.AuthorizationModel{
				SchemaVersion: SchemaVersion1_1,
				TypeDefinitions: []*openfgav1.TypeDefinition{
					{
						Type: "document",
						Relations: map[string]*openfgav1.Userset{
							"reader": {
								Userset: &openfgav1.Userset_Intersection{
									Intersection: &openfgav1.Usersets{
										Child: []*openfgav1.Userset{
											{
												Userset: &openfgav1.Userset_This{},
											},
											{
												Userset: &openfgav1.Userset_ComputedUserset{
													ComputedUserset: &openfgav1.ObjectRelation{Relation: "writer"},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			err: ErrRelationUndefined,
		},
		{
			name: "invalid_relation:_computedUserset_in_a_difference_base",
			model: &openfgav1.AuthorizationModel{
				SchemaVersion: SchemaVersion1_1,
				TypeDefinitions: []*openfgav1.TypeDefinition{
					{
						Type: "document",
						Relations: map[string]*openfgav1.Userset{
							"reader": {
								Userset: &openfgav1.Userset_Difference{
									Difference: &openfgav1.Difference{
										Base: &openfgav1.Userset{
											Userset: &openfgav1.Userset_ComputedUserset{
												ComputedUserset: &openfgav1.ObjectRelation{Relation: "writer"},
											},
										},
										Subtract: &openfgav1.Userset{
											Userset: &openfgav1.Userset_This{},
										},
									},
								},
							},
						},
					},
				},
			},
			err: ErrRelationUndefined,
		},
		{
			name: "invalid_relation:_computedUserset_in_a_difference_subtract",
			model: &openfgav1.AuthorizationModel{
				SchemaVersion: SchemaVersion1_1,
				TypeDefinitions: []*openfgav1.TypeDefinition{
					{
						Type: "document",
						Relations: map[string]*openfgav1.Userset{
							"reader": {
								Userset: &openfgav1.Userset_Difference{
									Difference: &openfgav1.Difference{
										Base: &openfgav1.Userset{
											Userset: &openfgav1.Userset_This{},
										},
										Subtract: &openfgav1.Userset{
											Userset: &openfgav1.Userset_ComputedUserset{
												ComputedUserset: &openfgav1.ObjectRelation{Relation: "writer"},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			err: ErrRelationUndefined,
		},
		{
			name: "invalid_relation:_tupleToUserset_where_tupleset_is_not_valid",
			model: &openfgav1.AuthorizationModel{
				SchemaVersion: SchemaVersion1_1,
				TypeDefinitions: []*openfgav1.TypeDefinition{
					{
						Type: "group",
						Relations: map[string]*openfgav1.Userset{
							"member": {
								Userset: &openfgav1.Userset_This{},
							},
						},
					},
					{
						Type: "document",
						Relations: map[string]*openfgav1.Userset{
							"reader": {
								Userset: &openfgav1.Userset_Union{
									Union: &openfgav1.Usersets{
										Child: []*openfgav1.Userset{
											{
												Userset: &openfgav1.Userset_This{},
											},
											{
												Userset: &openfgav1.Userset_TupleToUserset{
													TupleToUserset: &openfgav1.TupleToUserset{
														Tupleset: &openfgav1.ObjectRelation{
															Relation: "notavalidrelation",
														},
														ComputedUserset: &openfgav1.ObjectRelation{
															Relation: "member",
														},
													},
												},
											},
										},
									},
								},
							},
							"writer": {
								Userset: &openfgav1.Userset_This{},
							},
						},
					},
				},
			},
			err: ErrRelationUndefined,
		},
		{
			name: "invalid_relation:_tupleToUserset_where_computed_userset_is_not_valid",
			model: &openfgav1.AuthorizationModel{
				SchemaVersion: SchemaVersion1_1,
				TypeDefinitions: parser.MustParse(`
				type user

				type document
				  relations
				    define reader as notavalidrelation from writer
					define writer: [user] as self
				`),
			},
			err: ErrRelationUndefined,
		},
		{
			name: "Fails_If_Using_This_As_Relation_Name",
			model: &openfgav1.AuthorizationModel{
				SchemaVersion: SchemaVersion1_1,
				TypeDefinitions: []*openfgav1.TypeDefinition{
					{
						Type: "repo",
						Relations: map[string]*openfgav1.Userset{
							"this": This(),
						},
					},
				},
			},
			err: ErrReservedKeywords,
		},
		{
			name: "Fails_If_Using_Self_As_Relation_Name",
			model: &openfgav1.AuthorizationModel{
				SchemaVersion: SchemaVersion1_1,
				TypeDefinitions: []*openfgav1.TypeDefinition{
					{
						Type: "repo",
						Relations: map[string]*openfgav1.Userset{
							"self": This(),
						},
					},
				},
			},
			err: ErrReservedKeywords,
		},
		{
			name: "Fails_If_Using_This_As_Type_Name",
			model: &openfgav1.AuthorizationModel{
				SchemaVersion: SchemaVersion1_1,
				TypeDefinitions: []*openfgav1.TypeDefinition{
					{
						Type: "this",
						Relations: map[string]*openfgav1.Userset{
							"viewer": This(),
						},
					},
				},
			},
			err: ErrReservedKeywords,
		},
		{
			name: "Fails_If_Using_Self_As_Type_Name",
			model: &openfgav1.AuthorizationModel{
				SchemaVersion: SchemaVersion1_1,
				TypeDefinitions: []*openfgav1.TypeDefinition{
					{
						Type: "self",
						Relations: map[string]*openfgav1.Userset{
							"viewer": This(),
						},
					},
				},
			},
			err: ErrReservedKeywords,
		},
		{
			name: "Fails_If_Auth_Model_1.1_Has_A_Cycle_And_Only_One_Type",
			model: &openfgav1.AuthorizationModel{
				SchemaVersion: SchemaVersion1_1,
				TypeDefinitions: parser.MustParse(`
				type folder
				  relations
				    define parent: [folder] as self
					define viewer as viewer from parent
				`),
			},
			err: ErrNoEntrypoints,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewAndValidate(context.Background(), test.model)
			require.ErrorIs(t, err, test.err)
		})
	}
}
func TestSuccessfulRelationTypeRestrictionsValidations(t *testing.T) {
	var tests = []struct {
		name  string
		model *openfgav1.AuthorizationModel
	}{
		{
			name: "succeeds_on_a_valid_typeSystem_with_an_objectType_type",
			model: &openfgav1.AuthorizationModel{
				SchemaVersion: SchemaVersion1_1,
				TypeDefinitions: []*openfgav1.TypeDefinition{
					{
						Type: "user",
					},
					{
						Type: "document",
						Relations: map[string]*openfgav1.Userset{
							"reader": {Userset: &openfgav1.Userset_This{}},
						},
						Metadata: &openfgav1.Metadata{
							Relations: map[string]*openfgav1.RelationMetadata{
								"reader": {
									DirectlyRelatedUserTypes: []*openfgav1.RelationReference{
										{
											Type: "user",
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "succeeds_on_a_valid_typeSystem_with_a_type_and_type#relation_type",
			model: &openfgav1.AuthorizationModel{
				SchemaVersion: SchemaVersion1_1,
				TypeDefinitions: []*openfgav1.TypeDefinition{
					{
						Type: "user",
					},
					{
						Type: "group",
						Relations: map[string]*openfgav1.Userset{
							"admin":  {Userset: &openfgav1.Userset_This{}},
							"member": {Userset: &openfgav1.Userset_This{}},
						},
						Metadata: &openfgav1.Metadata{
							Relations: map[string]*openfgav1.RelationMetadata{
								"admin": {
									DirectlyRelatedUserTypes: []*openfgav1.RelationReference{
										{
											Type: "user",
										},
									},
								},
								"member": {
									DirectlyRelatedUserTypes: []*openfgav1.RelationReference{
										{
											Type: "user",
										},
									},
								},
							},
						},
					},
					{
						Type: "document",
						Relations: map[string]*openfgav1.Userset{
							"reader": {Userset: &openfgav1.Userset_This{}},
							"writer": {Userset: &openfgav1.Userset_This{}},
						},
						Metadata: &openfgav1.Metadata{
							Relations: map[string]*openfgav1.RelationMetadata{
								"reader": {
									DirectlyRelatedUserTypes: []*openfgav1.RelationReference{
										DirectRelationReference("user", ""),
										DirectRelationReference("group", "member"),
									},
								},
								"writer": {
									DirectlyRelatedUserTypes: []*openfgav1.RelationReference{
										DirectRelationReference("user", ""),
										DirectRelationReference("group", "admin"),
									},
								},
							},
						},
					},
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewAndValidate(context.Background(), test.model)
			require.NoError(t, err)
		})
	}
}

func TestInvalidRelationTypeRestrictionsValidations(t *testing.T) {
	var tests = []struct {
		name  string
		model *openfgav1.AuthorizationModel
		err   error
	}{
		{
			name: "relational_type_which_does_not_exist",
			model: &openfgav1.AuthorizationModel{
				SchemaVersion: SchemaVersion1_1,
				TypeDefinitions: []*openfgav1.TypeDefinition{
					{
						Type: "document",
						Relations: map[string]*openfgav1.Userset{
							"reader": {Userset: &openfgav1.Userset_This{}},
						},
						Metadata: &openfgav1.Metadata{
							Relations: map[string]*openfgav1.RelationMetadata{
								"reader": {
									DirectlyRelatedUserTypes: []*openfgav1.RelationReference{
										{
											Type: "group",
										},
									},
								},
							},
						},
					},
				},
			},
			err: InvalidRelationTypeError("document", "reader", "group", ""),
		},
		{
			name: "relation_type_of_form_type#relation_where_relation_doesn't_exist",
			model: &openfgav1.AuthorizationModel{
				SchemaVersion: SchemaVersion1_1,
				TypeDefinitions: []*openfgav1.TypeDefinition{
					{
						Type: "group",
					},
					{
						Type: "document",
						Relations: map[string]*openfgav1.Userset{
							"reader": {Userset: &openfgav1.Userset_This{}},
						},
						Metadata: &openfgav1.Metadata{
							Relations: map[string]*openfgav1.RelationMetadata{
								"reader": {
									DirectlyRelatedUserTypes: []*openfgav1.RelationReference{
										{
											Type:               "group",
											RelationOrWildcard: &openfgav1.RelationReference_Relation{Relation: "admin"},
										},
									},
								},
							},
						},
					},
				},
			},
			err: InvalidRelationTypeError("document", "reader", "group", "admin"),
		},
		{
			name: "assignable_relation_with_no_type:_this",
			model: &openfgav1.AuthorizationModel{
				SchemaVersion: SchemaVersion1_1,
				TypeDefinitions: []*openfgav1.TypeDefinition{
					{
						Type: "document",
						Relations: map[string]*openfgav1.Userset{
							"reader": {
								Userset: &openfgav1.Userset_This{},
							},
						},
					},
				},
			},
			err: AssignableRelationError("document", "reader"),
		},
		{
			name: "assignable_relation_with_no_type:_union",
			model: &openfgav1.AuthorizationModel{
				SchemaVersion: SchemaVersion1_1,
				TypeDefinitions: []*openfgav1.TypeDefinition{
					{
						Type: "user",
					},
					{
						Type: "document",
						Relations: map[string]*openfgav1.Userset{
							"writer": {
								Userset: &openfgav1.Userset_This{},
							},
							"reader": {
								Userset: &openfgav1.Userset_Union{
									Union: &openfgav1.Usersets{
										Child: []*openfgav1.Userset{
											{
												Userset: &openfgav1.Userset_This{},
											},
											{
												Userset: &openfgav1.Userset_ComputedUserset{
													ComputedUserset: &openfgav1.ObjectRelation{
														Relation: "writer",
													},
												},
											},
										},
									},
								},
							},
						},
						Metadata: &openfgav1.Metadata{
							Relations: map[string]*openfgav1.RelationMetadata{
								"writer": {
									DirectlyRelatedUserTypes: []*openfgav1.RelationReference{
										{
											Type: "user",
										},
									},
								},
							},
						},
					},
				},
			},
			err: AssignableRelationError("document", "reader"),
		},
		{
			name: "assignable_relation_wit_no_type:_intersection",
			model: &openfgav1.AuthorizationModel{
				SchemaVersion: SchemaVersion1_1,
				TypeDefinitions: []*openfgav1.TypeDefinition{
					{
						Type: "user",
					},
					{
						Type: "document",
						Relations: map[string]*openfgav1.Userset{
							"writer": {
								Userset: &openfgav1.Userset_This{},
							},
							"reader": {
								Userset: &openfgav1.Userset_Intersection{
									Intersection: &openfgav1.Usersets{
										Child: []*openfgav1.Userset{
											{
												Userset: &openfgav1.Userset_This{},
											},
											{
												Userset: &openfgav1.Userset_ComputedUserset{
													ComputedUserset: &openfgav1.ObjectRelation{
														Relation: "writer",
													},
												},
											},
										},
									},
								},
							},
						},
						Metadata: &openfgav1.Metadata{
							Relations: map[string]*openfgav1.RelationMetadata{
								"writer": {
									DirectlyRelatedUserTypes: []*openfgav1.RelationReference{
										{
											Type: "user",
										},
									},
								},
							},
						},
					},
				},
			},
			err: AssignableRelationError("document", "reader"),
		},
		{
			name: "assignable_relation_with_no_type:_difference base",
			model: &openfgav1.AuthorizationModel{
				SchemaVersion: SchemaVersion1_1,
				TypeDefinitions: []*openfgav1.TypeDefinition{
					{
						Type: "user",
					},
					{
						Type: "document",
						Relations: map[string]*openfgav1.Userset{
							"writer": {
								Userset: &openfgav1.Userset_This{},
							},
							"reader": {
								Userset: &openfgav1.Userset_Difference{
									Difference: &openfgav1.Difference{
										Base: &openfgav1.Userset{
											Userset: &openfgav1.Userset_This{},
										},
										Subtract: &openfgav1.Userset{
											Userset: &openfgav1.Userset_ComputedUserset{
												ComputedUserset: &openfgav1.ObjectRelation{
													Relation: "writer",
												},
											},
										},
									},
								},
							},
						},
						Metadata: &openfgav1.Metadata{
							Relations: map[string]*openfgav1.RelationMetadata{
								"writer": {
									DirectlyRelatedUserTypes: []*openfgav1.RelationReference{
										{
											Type: "user",
										},
									},
								},
							},
						},
					},
				},
			},
			err: AssignableRelationError("document", "reader"),
		},
		{
			name: "assignable_relation_with_no_type:_difference_subtract",
			model: &openfgav1.AuthorizationModel{
				SchemaVersion: SchemaVersion1_1,
				TypeDefinitions: []*openfgav1.TypeDefinition{
					{
						Type: "user",
					},
					{
						Type: "document",
						Relations: map[string]*openfgav1.Userset{
							"writer": {
								Userset: &openfgav1.Userset_This{},
							},
							"reader": {
								Userset: &openfgav1.Userset_Difference{
									Difference: &openfgav1.Difference{
										Base: &openfgav1.Userset{
											Userset: &openfgav1.Userset_ComputedUserset{
												ComputedUserset: &openfgav1.ObjectRelation{
													Relation: "writer",
												},
											},
										},
										Subtract: &openfgav1.Userset{
											Userset: &openfgav1.Userset_This{},
										},
									},
								},
							},
						},
						Metadata: &openfgav1.Metadata{
							Relations: map[string]*openfgav1.RelationMetadata{
								"writer": {
									DirectlyRelatedUserTypes: []*openfgav1.RelationReference{
										{
											Type: "user",
										},
									},
								},
							},
						},
					},
				},
			},
			err: AssignableRelationError("document", "reader"),
		},
		{
			name: "non-assignable_relation_with_a_type",
			model: &openfgav1.AuthorizationModel{
				SchemaVersion: SchemaVersion1_1,
				TypeDefinitions: []*openfgav1.TypeDefinition{
					{
						Type: "user",
					},
					{
						Type: "document",
						Relations: map[string]*openfgav1.Userset{
							"writer": {
								Userset: &openfgav1.Userset_This{},
							},
							"reader": {
								Userset: &openfgav1.Userset_ComputedUserset{
									ComputedUserset: &openfgav1.ObjectRelation{Relation: "writer"},
								},
							},
						},
						Metadata: &openfgav1.Metadata{
							Relations: map[string]*openfgav1.RelationMetadata{
								"writer": {
									DirectlyRelatedUserTypes: []*openfgav1.RelationReference{
										{
											Type: "user",
										},
									},
								},
								"reader": {
									DirectlyRelatedUserTypes: []*openfgav1.RelationReference{
										{
											Type: "user",
										},
									},
								},
							},
						},
					},
				},
			},
			err: NonAssignableRelationError("document", "reader"),
		},
		{
			name: "userset_specified_as_allowed_type_but_the_relation_is_used_in_a_TTU_rewrite",
			model: &openfgav1.AuthorizationModel{
				SchemaVersion: SchemaVersion1_1,
				TypeDefinitions: []*openfgav1.TypeDefinition{
					{
						Type: "user",
					},
					{
						Type: "folder",
						Relations: map[string]*openfgav1.Userset{
							"member": This(),
						},
						Metadata: &openfgav1.Metadata{
							Relations: map[string]*openfgav1.RelationMetadata{
								"member": {
									DirectlyRelatedUserTypes: []*openfgav1.RelationReference{
										DirectRelationReference("user", ""),
									},
								},
							},
						},
					},
					{
						Type: "document",
						Relations: map[string]*openfgav1.Userset{
							"parent":   This(),
							"can_view": TupleToUserset("parent", "member"),
						},
						Metadata: &openfgav1.Metadata{
							Relations: map[string]*openfgav1.RelationMetadata{
								"parent": {
									DirectlyRelatedUserTypes: []*openfgav1.RelationReference{
										DirectRelationReference("folder", "member"),
									},
								},
							},
						},
					},
				},
			},
			err: InvalidRelationTypeError("document", "parent", "folder", "member"),
		},
		{
			name: "userset_specified_as_allowed_type_but_the_relation_is_used_in_a_TTU_rewrite_included_in_a_union",
			model: &openfgav1.AuthorizationModel{
				SchemaVersion: SchemaVersion1_1,
				TypeDefinitions: []*openfgav1.TypeDefinition{
					{
						Type: "user",
					},
					{
						Type: "folder",
						Relations: map[string]*openfgav1.Userset{
							"parent": This(),
							"viewer": This(),
						},
						Metadata: &openfgav1.Metadata{
							Relations: map[string]*openfgav1.RelationMetadata{
								"parent": {
									DirectlyRelatedUserTypes: []*openfgav1.RelationReference{
										{
											Type: "folder",
										},
									},
								},
								"viewer": {
									DirectlyRelatedUserTypes: []*openfgav1.RelationReference{
										{
											Type: "user",
										},
									},
								},
							},
						},
					},
					{
						Type: "document",
						Relations: map[string]*openfgav1.Userset{
							"parent": This(),
							"viewer": Union(TupleToUserset("parent", "viewer"), This()),
						},
						Metadata: &openfgav1.Metadata{
							Relations: map[string]*openfgav1.RelationMetadata{
								"parent": {
									DirectlyRelatedUserTypes: []*openfgav1.RelationReference{
										DirectRelationReference("folder", "parent"),
									},
								},
								"viewer": {
									DirectlyRelatedUserTypes: []*openfgav1.RelationReference{
										DirectRelationReference("user", ""),
										DirectRelationReference("folder", "parent"),
									},
								},
							},
						},
					},
				},
			},
			err: InvalidRelationTypeError("document", "parent", "folder", "parent"),
		},
		{
			name: "WildcardNotAllowedInTheTuplesetPartOfTTU",
			model: &openfgav1.AuthorizationModel{
				SchemaVersion: SchemaVersion1_1,
				TypeDefinitions: []*openfgav1.TypeDefinition{
					{
						Type: "user",
					},
					{
						Type: "folder",
						Relations: map[string]*openfgav1.Userset{
							"viewer": This(),
						},
						Metadata: &openfgav1.Metadata{
							Relations: map[string]*openfgav1.RelationMetadata{
								"viewer": {
									DirectlyRelatedUserTypes: []*openfgav1.RelationReference{
										DirectRelationReference("user", ""),
									},
								},
							},
						},
					},
					{
						Type: "document",
						Relations: map[string]*openfgav1.Userset{
							"parent": This(),
							"viewer": Union(This(), TupleToUserset("parent", "viewer")),
						},
						Metadata: &openfgav1.Metadata{
							Relations: map[string]*openfgav1.RelationMetadata{
								"parent": {
									DirectlyRelatedUserTypes: []*openfgav1.RelationReference{
										WildcardRelationReference("folder"),
									},
								},
								"viewer": {
									DirectlyRelatedUserTypes: []*openfgav1.RelationReference{
										DirectRelationReference("user", ""),
									},
								},
							},
						},
					},
				},
			},
			err: InvalidRelationTypeError("document", "parent", "folder", ""),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewAndValidate(context.Background(), test.model)
			require.EqualError(t, err, test.err.Error())
		})
	}
}

func TestRelationInvolvesIntersection(t *testing.T) {
	tests := []struct {
		name        string
		model       string
		rr          *openfgav1.RelationReference
		expected    bool
		expectedErr error
	}{
		{
			name: "indirect_computeduserset_through_ttu_containing_intersection",
			model: `
			type user

			type folder
			  relations
			    define manage: [user] as self
			    define editor: [user] as self and manage

			type document
			  relations
			    define parent: [folder] as self
			    define editor as editor from parent
			    define viewer as editor
			`,
			rr:       DirectRelationReference("document", "viewer"),
			expected: true,
		},
		{
			name: "ttu_relations_containing_intersection",
			model: `
			type user

			type folder
			  relations
			    define editor: [user] as self
			    define viewer: [user] as self and editor

			type document
			  relations
			    define parent: [folder] as self
			    define viewer as viewer from parent
			`,
			rr:       DirectRelationReference("document", "viewer"),
			expected: true,
		},
		{
			name: "indirect_relations_containing_intersection",
			model: `
			type user

			type document
			  relations
			    define editor: [user] as self
			    define viewer: [user] as self and editor
			`,
			rr:       DirectRelationReference("document", "viewer"),
			expected: true,
		},
		{
			name: "undefined_type",
			model: `
			type user
			`,
			rr:          DirectRelationReference("document", "viewer"),
			expected:    false,
			expectedErr: ErrObjectTypeUndefined,
		},
		{
			name: "undefined_relation",
			model: `
			type user
			`,
			rr:          DirectRelationReference("user", "viewer"),
			expected:    false,
			expectedErr: ErrRelationUndefined,
		},
		{
			name: "non-assignable_indirect_type_restriction_involving_intersection",
			model: `
			type user

			type org
			  relations
			    define allowed: [user] as self
			    define dept: [group] as self
			    define dept_member as member from dept
			    define dept_allowed_member as dept_member and allowed

			type resource
			  relations
			    define reader: [user] as self or writer
			    define writer: [org#dept_allowed_member] as self
			`,
			rr:       DirectRelationReference("resource", "reader"),
			expected: true,
		},
		{
			name: "indirect_relationship_through_type_restriction",
			model: `
			type user

			type document
			  relations
			    define allowed: [user] as self
			    define editor: [user] as self and allowed
			    define viewer: [document#editor] as self
			`,
			rr:       DirectRelationReference("document", "viewer"),
			expected: true,
		},
		{
			name: "github_model",
			model: `
			type user

			type organization
			  relations
			    define member: [user] as self or owner
				define owner: [user] as self
				define repo_admin: [user, organization#member] as self
				define repo_reader: [user, organization#member] as self
				define repo_writer: [user, organization#member] as self

			type team
			  relations
			    define member: [user, team#member] as self

			type repo
			  relations
			    define admin: [user, team#member] as self or repo_admin from owner
				define maintainer: [user, team#member] as self or admin
				define owner: [organization] as self
				define reader: [user, team#member] as self or triager or repo_reader from owner
				define triager: [user, team#member] as self or writer
				define writer: [user, team#member] as self or maintainer or repo_writer from owner
			`,
			rr:       DirectRelationReference("repo", "admin"),
			expected: false,
		},
		{
			name: "github_model",
			model: `
			type user

			type organization
			  relations
			    define member: [user] as self or owner
				define owner: [user] as self
				define repo_admin: [user, organization#member] as self
				define repo_reader: [user, organization#member] as self
				define repo_writer: [user, organization#member] as self

			type team
			  relations
			    define member: [user, team#member] as self

			type repo
			  relations
			    define admin: [user, team#member] as self or repo_admin from owner
				define maintainer: [user, team#member] as self or admin
				define owner: [organization] as self
				define reader: [user, team#member] as self or triager or repo_reader from owner
				define triager: [user, team#member] as self or writer
				define writer: [user, team#member] as self or maintainer or repo_writer from owner
			`,
			rr:       DirectRelationReference("repo", "admin"),
			expected: false,
		},
		{
			name: "direct_relations_related_to_each_other",
			model: `
			type user

			type example
			  relations
			    define editor: [example#viewer] as self
			    define viewer: [example#editor] as self
			`,
			rr:       DirectRelationReference("example", "editor"),
			expected: false,
		},
		{
			name: "cyclical_evaluation_of_tupleset",
			model: `
			type user

			type node
			  relations
			    define parent: [node] as self
			    define editor: [user] as self or editor from parent
			`,
			rr:       DirectRelationReference("node", "editor"),
			expected: false,
		},
		{
			name: "nested_intersection_1",
			model: `
			type user

			type folder
			  relations
			    define allowed: [user] as self
			    define viewer: [user] as self and allowed

			type document
			  relations
			    define parent: [folder] as self
				define viewer as viewer from parent
			`,
			rr:       DirectRelationReference("document", "viewer"),
			expected: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {

			typedefs := parser.MustParse(test.model)

			typesys := New(&openfgav1.AuthorizationModel{
				TypeDefinitions: typedefs,
			})

			objectType := test.rr.GetType()
			relationStr := test.rr.GetRelation()

			actual, err := typesys.RelationInvolvesIntersection(objectType, relationStr)
			require.ErrorIs(t, err, test.expectedErr)
			require.Equal(t, test.expected, actual)
		})
	}
}

func TestRelationInvolvesExclusion(t *testing.T) {

	tests := []struct {
		name        string
		model       string
		rr          *openfgav1.RelationReference
		expected    bool
		expectedErr error
	}{
		{
			name: "indirect_computed_userset_through_ttu_containing_exclusion",
			model: `
			type user

			type folder
			  relations
			    define restricted: [user] as self
			    define editor: [user] as self but not restricted

			type document
			  relations
			    define parent: [folder] as self
			    define editor as editor from parent
			    define viewer as editor
			`,
			rr:       DirectRelationReference("document", "viewer"),
			expected: true,
		},
		{
			name: "ttu_relations_containing_exclusion",
			model: `
			type user

			type folder
			  relations
			    define restricted: [user] as self
			    define viewer: [user] as self but not restricted

			type document
			  relations
			    define parent: [folder] as self
			    define viewer as viewer from parent
			`,
			rr:       DirectRelationReference("document", "viewer"),
			expected: true,
		},
		{
			name: "indirect_relations_containing_exclusion",
			model: `
			type user

			type document
			  relations
			    define restricted: [user] as self
			    define editor: [user] as self but not restricted
			    define viewer as editor
			`,
			rr:       DirectRelationReference("document", "viewer"),
			expected: true,
		},
		{
			name: "undefined_type",
			model: `
			type user
			`,
			rr:          DirectRelationReference("document", "viewer"),
			expected:    false,
			expectedErr: ErrObjectTypeUndefined,
		},
		{
			name: "undefined_relation",
			model: `
			type user
			`,
			rr:          DirectRelationReference("user", "viewer"),
			expected:    false,
			expectedErr: ErrRelationUndefined,
		},
		{
			name: "non-assignable_indirect_type_restriction_involving_exclusion",
			model: `
			type user

			type org
			  relations
			    define removed: [user] as self
			    define dept: [group] as self
			    define dept_member as member from dept
			    define dept_allowed_member as dept_member but not removed

			type resource
			  relations
			    define reader: [user] as self or writer
			    define writer: [org#dept_allowed_member] as self
			`,
			rr:       DirectRelationReference("resource", "reader"),
			expected: true,
		},
		{
			name: "indirect_relationship_through_type_restriction",
			model: `
			type user

			type document
			  relations
			    define restricted: [user] as self
			    define editor: [user] as self but not restricted
			    define viewer: [document#editor] as self
			`,
			rr:       DirectRelationReference("document", "viewer"),
			expected: true,
		},
		{
			name: "direct_relations_related_to_each_other",
			model: `
			type user

			type example
			  relations
			    define editor: [example#viewer] as self
			    define viewer: [example#editor] as self
			`,
			rr:       DirectRelationReference("example", "editor"),
			expected: false,
		},
		{
			name: "cyclical_evaluation_of_tupleset",
			model: `
			type user

			type node
			  relations
			    define parent: [node] as self
			    define editor: [user] as self or editor from parent
			`,
			rr:       DirectRelationReference("node", "editor"),
			expected: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {

			typedefs := parser.MustParse(test.model)

			typesys := New(&openfgav1.AuthorizationModel{
				TypeDefinitions: typedefs,
			})

			objectType := test.rr.GetType()
			relationStr := test.rr.GetRelation()

			actual, err := typesys.RelationInvolvesExclusion(objectType, relationStr)
			require.ErrorIs(t, err, test.expectedErr)
			require.Equal(t, test.expected, actual)
		})
	}
}

func TestIsTuplesetRelation(t *testing.T) {

	tests := []struct {
		name          string
		model         *openfgav1.AuthorizationModel
		objectType    string
		relation      string
		expected      bool
		expectedError error
	}{
		{
			name:          "undefined_object_type_returns_error",
			objectType:    "document",
			relation:      "viewer",
			expected:      false,
			expectedError: ErrObjectTypeUndefined,
		},
		{
			name: "undefined_relation_returns_error",
			model: &openfgav1.AuthorizationModel{
				TypeDefinitions: []*openfgav1.TypeDefinition{
					{
						Type: "document",
					},
				},
			},
			objectType:    "document",
			relation:      "viewer",
			expected:      false,
			expectedError: ErrRelationUndefined,
		},
		{
			name: "direct_tupleset_relation",
			model: &openfgav1.AuthorizationModel{
				TypeDefinitions: []*openfgav1.TypeDefinition{
					{
						Type: "document",
						Relations: map[string]*openfgav1.Userset{
							"parent": This(),
							"viewer": TupleToUserset("parent", "viewer"),
						},
					},
				},
			},
			objectType: "document",
			relation:   "parent",
			expected:   true,
		},
		{
			name: "tupleset_relation_under_union",
			model: &openfgav1.AuthorizationModel{
				TypeDefinitions: []*openfgav1.TypeDefinition{
					{
						Type: "document",
						Relations: map[string]*openfgav1.Userset{
							"parent": This(),
							"viewer": Union(
								This(),
								TupleToUserset("parent", "viewer"),
							),
						},
					},
				},
			},
			objectType: "document",
			relation:   "parent",
			expected:   true,
		},
		{
			name: "tupleset_relation_under_intersection",
			model: &openfgav1.AuthorizationModel{
				TypeDefinitions: []*openfgav1.TypeDefinition{
					{
						Type: "document",
						Relations: map[string]*openfgav1.Userset{
							"parent": This(),
							"viewer": Intersection(
								This(),
								TupleToUserset("parent", "viewer"),
							),
						},
					},
				},
			},
			objectType: "document",
			relation:   "parent",
			expected:   true,
		},
		{
			name: "tupleset_relation_under_exclusion",
			model: &openfgav1.AuthorizationModel{
				TypeDefinitions: []*openfgav1.TypeDefinition{
					{
						Type: "document",
						Relations: map[string]*openfgav1.Userset{
							"parent": This(),
							"viewer": Difference(
								This(),
								TupleToUserset("parent", "viewer"),
							),
						},
					},
				},
			},
			objectType: "document",
			relation:   "parent",
			expected:   true,
		},
		{
			name: "tupleset_relation_under_nested_union",
			model: &openfgav1.AuthorizationModel{
				TypeDefinitions: []*openfgav1.TypeDefinition{
					{
						Type: "document",
						Relations: map[string]*openfgav1.Userset{
							"parent": This(),
							"viewer": Intersection(
								This(),
								Union(TupleToUserset("parent", "viewer")),
							),
						},
					},
				},
			},
			objectType: "document",
			relation:   "parent",
			expected:   true,
		},
		{
			name: "tupleset_relation_under_nested_intersection",
			model: &openfgav1.AuthorizationModel{
				TypeDefinitions: []*openfgav1.TypeDefinition{
					{
						Type: "document",
						Relations: map[string]*openfgav1.Userset{
							"parent": This(),
							"viewer": Union(
								This(),
								Intersection(TupleToUserset("parent", "viewer")),
							),
						},
					},
				},
			},
			objectType: "document",
			relation:   "parent",
			expected:   true,
		},
		{
			name: "tupleset_relation_under_nested_exclusion",
			model: &openfgav1.AuthorizationModel{
				TypeDefinitions: []*openfgav1.TypeDefinition{
					{
						Type: "document",
						Relations: map[string]*openfgav1.Userset{
							"parent": This(),
							"viewer": Union(
								This(),
								Difference(This(), TupleToUserset("parent", "viewer")),
							),
						},
					},
				},
			},
			objectType: "document",
			relation:   "parent",
			expected:   true,
		},
		{
			name: "not_a_tupleset_relation",
			model: &openfgav1.AuthorizationModel{
				TypeDefinitions: []*openfgav1.TypeDefinition{
					{
						Type: "document",
						Relations: map[string]*openfgav1.Userset{
							"parent": This(),
							"viewer": TupleToUserset("parent", "viewer"),
						},
					},
				},
			},
			objectType: "document",
			relation:   "viewer",
			expected:   false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			typesys := New(test.model)

			actual, err := typesys.IsTuplesetRelation(test.objectType, test.relation)
			require.ErrorIs(t, err, test.expectedError)
			require.Equal(t, test.expected, actual)
		})
	}
}

func TestIsDirectlyRelated(t *testing.T) {
	tests := []struct {
		name   string
		model  string
		target *openfgav1.RelationReference
		source *openfgav1.RelationReference
		result bool
	}{
		{
			name: "wildcard_and_wildcard",
			model: `
			type user

			type document
			  relations
			    define viewer: [user:*] as self
			`,
			target: DirectRelationReference("document", "viewer"),
			source: WildcardRelationReference("user"),
			result: true,
		},
		{
			name: "wildcard_and_direct",
			model: `
			type user

			type document
			  relations
			    define viewer: [user:*] as self
			`,
			target: DirectRelationReference("document", "viewer"),
			source: DirectRelationReference("user", ""),
			result: false,
		},
		{
			name: "direct_and_wildcard",
			model: `
			type user
			
			type document
			  relations
			    define viewer: [user] as self
			`,
			target: DirectRelationReference("document", "viewer"),
			source: WildcardRelationReference("user"),
			result: false,
		},
		{
			name: "direct_type",
			model: `
			type user
			
			type document
			  relations
			    define viewer: [user] as self
			`,
			target: DirectRelationReference("document", "viewer"),
			source: DirectRelationReference("user", ""),
			result: true,
		},
		{
			name: "relation_not_related",
			model: `
			type user
			  relations
			    define manager: [user] as self
			
			type document
			  relations
			    define viewer: [user] as self
			`,
			target: DirectRelationReference("document", "viewer"),
			source: DirectRelationReference("user", "manager"),
			result: false,
		},
		{
			name: "direct_and_userset",
			model: `
			type group
			  relations
			    define member: [group#member] as self
			
			type document
			  relations
			    define viewer: [group#member] as self
			`,
			target: DirectRelationReference("document", "viewer"),
			source: DirectRelationReference("group", "member"),
			result: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {

			typedefs := parser.MustParse(test.model)
			typesys := New(&openfgav1.AuthorizationModel{
				SchemaVersion:   SchemaVersion1_1,
				TypeDefinitions: typedefs,
			})

			ok, err := typesys.IsDirectlyRelated(test.target, test.source)
			require.NoError(t, err)
			require.Equal(t, test.result, ok)
		})
	}
}

func TestIsPubliclyAssignable(t *testing.T) {
	tests := []struct {
		name       string
		model      string
		target     *openfgav1.RelationReference
		objectType string
		result     bool
	}{
		{
			name: "1",
			model: `
			type user

			type document
			  relations
			    define viewer: [user:*] as self
			`,
			target:     DirectRelationReference("document", "viewer"),
			objectType: "user",
			result:     true,
		},
		{
			name: "2",
			model: `
			type user

			type document
			  relations
			    define viewer: [user] as self
			`,
			target:     DirectRelationReference("document", "viewer"),
			objectType: "user",
			result:     false,
		},
		{
			name: "3",
			model: `
			type user
			type employee

			type document
			  relations
			    define viewer: [employee:*] as self
			`,
			target:     DirectRelationReference("document", "viewer"),
			objectType: "user",
			result:     false,
		},
		{
			name: "4",
			model: `
			type user

			type group
			  relations
			    define member: [user:*] as self

			type document
			  relations
			    define viewer: [group#member] as self
			`,
			target:     DirectRelationReference("document", "viewer"),
			objectType: "user",
			result:     false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {

			typedefs := parser.MustParse(test.model)
			typesys := New(&openfgav1.AuthorizationModel{
				SchemaVersion:   SchemaVersion1_1,
				TypeDefinitions: typedefs,
			})

			ok, err := typesys.IsPubliclyAssignable(test.target, test.objectType)
			require.NoError(t, err)
			require.Equal(t, ok, test.result)
		})
	}
}

func TestRewriteContainsExclusion(t *testing.T) {
	tests := []struct {
		name     string
		model    string
		rr       *openfgav1.RelationReference
		expected bool
	}{
		{
			name: "simple_exclusion",
			model: `
			type user

			type folder
			  relations
			    define restricted: [user] as self
			    define editor: [user] as self
			    define viewer: [user] as (self or editor) but not restricted
			`,
			rr:       DirectRelationReference("folder", "viewer"),
			expected: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {

			typedefs := parser.MustParse(test.model)

			typesys := New(&openfgav1.AuthorizationModel{
				TypeDefinitions: typedefs,
			})

			rel, err := typesys.GetRelation(test.rr.GetType(), test.rr.GetRelation())
			require.NoError(t, err)

			actual := RewriteContainsExclusion(rel.GetRewrite())
			require.Equal(t, test.expected, actual)
		})
	}
}

func TestRewriteContainsIntersection(t *testing.T) {
	tests := []struct {
		name     string
		model    string
		rr       *openfgav1.RelationReference
		expected bool
	}{
		{
			name: "simple_intersection",
			model: `
			type user

			type folder
			  relations
			    define allowed: [user] as self
			    define editor: [user] as self
			    define viewer: [user] as (self or editor) and allowed
			`,
			rr:       DirectRelationReference("folder", "viewer"),
			expected: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {

			typedefs := parser.MustParse(test.model)

			typesys := New(&openfgav1.AuthorizationModel{
				TypeDefinitions: typedefs,
			})

			rel, err := typesys.GetRelation(test.rr.GetType(), test.rr.GetRelation())
			require.NoError(t, err)

			actual := RewriteContainsIntersection(rel.GetRewrite())
			require.Equal(t, test.expected, actual)
		})
	}
}

func TestGetRelationReferenceAsString(t *testing.T) {
	require.Equal(t, "", GetRelationReferenceAsString(nil))
	require.Equal(t, "team#member", GetRelationReferenceAsString(DirectRelationReference("team", "member")))
	require.Equal(t, "team:*", GetRelationReferenceAsString(WildcardRelationReference("team")))
}

func TestDirectlyRelatedUsersets(t *testing.T) {
	tests := []struct {
		name       string
		model      string
		objectType string
		relation   string
		expected   []*openfgav1.RelationReference
	}{
		{
			name: "only_direct_relation",
			model: `type user

			type folder
			  relations
			    define allowed: [user] as self`,
			objectType: "folder",
			relation:   "allowed",
			expected:   nil,
		},
		{
			name: "with_public_relation",
			model: `type user

			type folder
			  relations
			    define allowed: [user, user:*] as self`,
			objectType: "folder",
			relation:   "allowed",
			expected: []*openfgav1.RelationReference{
				WildcardRelationReference("user"),
			},
		},
		{
			name: "with_ttu_relation",
			model: `type user
            type group
              relations
                define member: [user] as self

			type folder
			  relations
			    define allowed: [group#member] as self`,
			objectType: "folder",
			relation:   "allowed",
			expected: []*openfgav1.RelationReference{
				DirectRelationReference("group", "member"),
			},
		},
		{
			name: "mix_direct_and_public_relation",
			model: `type user
            type group
              relations
                define member: [user] as self

			type folder
			  relations
			    define allowed: [group#member, user] as self`,
			objectType: "folder",
			relation:   "allowed",
			expected: []*openfgav1.RelationReference{
				DirectRelationReference("group", "member"),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {

			typedefs := parser.MustParse(test.model)

			typesys := New(&openfgav1.AuthorizationModel{
				TypeDefinitions: typedefs,
			})
			result, err := typesys.DirectlyRelatedUsersets(test.objectType, test.relation)
			require.NoError(t, err)
			require.Equal(t, test.expected, result)
		})
	}
}

func TestHasTypeInfo(t *testing.T) {
	tests := []struct {
		name       string
		schema     string
		model      string
		objectType string
		relation   string
		expected   bool
	}{
		{
			name:   "has_type_info_true",
			schema: SchemaVersion1_1,
			model: `type user

			type folder
			  relations
			    define allowed: [user] as self`,
			objectType: "folder",
			relation:   "allowed",
			expected:   true,
		},
		{
			name:   "has_type_info_false",
			schema: SchemaVersion1_0,
			model: `type user

			type folder
			  relations
			    define allowed as self`,
			objectType: "folder",
			relation:   "allowed",
			expected:   false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {

			typedefs := parser.MustParse(test.model)

			typesys := New(&openfgav1.AuthorizationModel{
				SchemaVersion:   test.schema,
				TypeDefinitions: typedefs,
			})
			result, err := typesys.HasTypeInfo(test.objectType, test.relation)
			require.NoError(t, err)
			require.Equal(t, test.expected, result)
		})
	}
}

var (
	RelationshipEdgeTransformer = cmp.Transformer("Sort", func(in []*RelationshipEdge) []*RelationshipEdge {
		out := append([]*RelationshipEdge(nil), in...) // Copy input to avoid mutating it

		// Sort by Type and then by edge and then by tupleset relation
		sort.SliceStable(out, func(i, j int) bool {
			if out[i].Type > out[j].Type {
				return false
			}

			if GetRelationReferenceAsString(out[i].TargetReference) > GetRelationReferenceAsString(out[j].TargetReference) {
				return false
			}

			if GetRelationReferenceAsString(out[i].TuplesetRelation) > GetRelationReferenceAsString(out[j].TuplesetRelation) {
				return false
			}

			return true
		})

		return out
	})
)

func TestRelationshipEdge_String(t *testing.T) {
	for _, tc := range []struct {
		name             string
		expected         string
		relationshipEdge RelationshipEdge
	}{
		{
			name:     "TupleToUsersetEdge",
			expected: "userset type:\"document\" relation:\"viewer\", type ttu, tupleset type:\"document\" relation:\"parent\"",
			relationshipEdge: RelationshipEdge{
				Type:             TupleToUsersetEdge,
				TargetReference:  DirectRelationReference("document", "viewer"),
				TuplesetRelation: DirectRelationReference("document", "parent"),
				Condition:        NoFurtherEvalCondition,
			},
		},
		{
			name:     "ComputedUsersetEdge",
			expected: "userset type:\"document\" relation:\"viewer\", type computed_userset",
			relationshipEdge: RelationshipEdge{
				Type:            ComputedUsersetEdge,
				TargetReference: DirectRelationReference("document", "viewer"),
				Condition:       NoFurtherEvalCondition,
			},
		},
		{
			name:     "DirectEdge",
			expected: "userset type:\"document\" relation:\"viewer\", type direct",
			relationshipEdge: RelationshipEdge{
				Type:            DirectEdge,
				TargetReference: DirectRelationReference("document", "viewer"),
				Condition:       NoFurtherEvalCondition,
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expected, tc.relationshipEdge.String())
		})
	}
}

func TestRelationshipEdgeType_String(t *testing.T) {

	require.Equal(t, "direct", DirectEdge.String())
	require.Equal(t, "computed_userset", ComputedUsersetEdge.String())
	require.Equal(t, "ttu", TupleToUsersetEdge.String())
	require.Equal(t, "undefined", RelationshipEdgeType(4).String())
}

func TestPrunedRelationshipEdges(t *testing.T) {

	tests := []struct {
		name     string
		model    string
		target   *openfgav1.RelationReference
		source   *openfgav1.RelationReference
		expected []*RelationshipEdge
	}{
		{
			name: "basic_intersection",
			model: `
			type user

			type document
			  relations
			    define allowed: [user] as self
			    define viewer: [user] as self and allowed
			`,
			target: DirectRelationReference("document", "viewer"),
			source: DirectRelationReference("user", ""),
			expected: []*RelationshipEdge{
				{
					Type:            DirectEdge,
					TargetReference: DirectRelationReference("document", "viewer"),
					Condition:       RequiresFurtherEvalCondition,
				},
			},
		},
		{
			name: "basic_intersection_through_ttu_1",
			model: `
			type user

			type folder
			  relations
			    define allowed: [user] as self
			    define viewer: [user] as self and allowed

			type document
			  relations
			    define parent: [folder] as self
			    define viewer as viewer from parent
			`,
			target: DirectRelationReference("document", "viewer"),
			source: DirectRelationReference("user", ""),
			expected: []*RelationshipEdge{
				{
					Type:            DirectEdge,
					TargetReference: DirectRelationReference("folder", "viewer"),
					Condition:       RequiresFurtherEvalCondition,
				},
			},
		},
		{
			name: "basic_intersection_through_ttu_2",
			model: `
			type user

			type organization
			  relations
			    define allowed: [user] as self
			    define viewer: [user] as self and allowed

			type folder
			  relations
			    define parent: [organization] as self
			    define viewer as viewer from parent

			type document
			  relations
			    define parent: [folder] as self
			    define viewer as viewer from parent
			`,
			target: DirectRelationReference("document", "viewer"),
			source: DirectRelationReference("folder", "viewer"),
			expected: []*RelationshipEdge{
				{
					Type:             TupleToUsersetEdge,
					TargetReference:  DirectRelationReference("document", "viewer"),
					TuplesetRelation: DirectRelationReference("document", "parent"),
					Condition:        RequiresFurtherEvalCondition,
				},
			},
		},
		{
			name: "basic_exclusion_through_ttu_1",
			model: `
			type user

			type folder
			  relations
			    define writer: [user] as self
			    define editor: [user] as self
			    define viewer as writer but not editor

			type document
			  relations
			    define parent: [folder] as self
			    define viewer as viewer from parent
			`,
			target: DirectRelationReference("document", "viewer"),
			source: DirectRelationReference("user", ""),
			expected: []*RelationshipEdge{
				{
					Type:            DirectEdge,
					TargetReference: DirectRelationReference("folder", "writer"),
					Condition:       RequiresFurtherEvalCondition,
				},
			},
		},
		{
			name: "basic_exclusion_through_ttu_2",
			model: `
			type user

			type folder
			  relations
			    define writer: [user] as self
			    define editor: [user] as self
			    define viewer as writer but not editor

			type document
			  relations
			    define parent: [folder] as self
			    define viewer as viewer from parent
			`,
			target: DirectRelationReference("document", "viewer"),
			source: DirectRelationReference("folder", "viewer"),
			expected: []*RelationshipEdge{
				{
					Type:             TupleToUsersetEdge,
					TargetReference:  DirectRelationReference("document", "viewer"),
					TuplesetRelation: DirectRelationReference("document", "parent"),
					Condition:        RequiresFurtherEvalCondition,
				},
			},
		},
		{
			name: "ttu_with_indirect",
			model: `
			  type user
	
			  type repo
				relations
				  define admin: [user] as self or repo_admin from owner
				  define owner: [organization] as self
	
			  type organization
				relations
				  define member: [user] as self or owner
				  define owner: [user] as self
				  define repo_admin: [user, organization#member] as self
			`,
			target: DirectRelationReference("repo", "admin"),
			source: DirectRelationReference("organization", "member"),
			expected: []*RelationshipEdge{
				{
					Type:            DirectEdge,
					TargetReference: DirectRelationReference("organization", "repo_admin"),
					Condition:       NoFurtherEvalCondition,
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			typedefs := parser.MustParse(test.model)
			typesys := New(&openfgav1.AuthorizationModel{
				SchemaVersion:   SchemaVersion1_1,
				TypeDefinitions: typedefs,
			})

			g := NewGraph(typesys)

			edges, err := g.GetPrunedRelationshipEdges(test.target, test.source)
			require.NoError(t, err)

			cmpOpts := []cmp.Option{
				cmpopts.IgnoreUnexported(openfgav1.RelationReference{}),
				RelationshipEdgeTransformer,
			}
			if diff := cmp.Diff(test.expected, edges, cmpOpts...); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestRelationshipEdges(t *testing.T) {

	tests := []struct {
		name     string
		model    string
		target   *openfgav1.RelationReference
		source   *openfgav1.RelationReference
		expected []*RelationshipEdge
	}{
		{
			name: "direct_edge_through_ComputedUserset_with_multiple_type_restrictions",
			model: `
			type user

			type group
			  relations
			    define member: [user, group#member] as self

			type document
			  relations
			    define editor: [user, group#member] as self
			    define viewer as editor
			`,
			target: DirectRelationReference("document", "viewer"),
			source: DirectRelationReference("user", ""),
			expected: []*RelationshipEdge{
				{
					Type:            DirectEdge,
					TargetReference: DirectRelationReference("document", "editor"),
					Condition:       NoFurtherEvalCondition,
				},
				{
					Type:            DirectEdge,
					TargetReference: DirectRelationReference("group", "member"),
					Condition:       NoFurtherEvalCondition,
				},
			},
		},
		{
			name: "direct_edge_through_ComputedUserset",
			model: `
			type user

			type document
			  relations
			    define editor: [user] as self
			    define viewer as editor
			`,
			target: DirectRelationReference("document", "viewer"),
			source: DirectRelationReference("user", ""),
			expected: []*RelationshipEdge{
				{
					Type:            DirectEdge,
					TargetReference: DirectRelationReference("document", "editor"),
					Condition:       NoFurtherEvalCondition,
				},
			},
		},
		{
			name: "direct_edge_through_TupleToUserset_with_multiple_type_restrictions",
			model: `
			type user

			type group
			  relations
			    define member: [user] as self

			type folder
			  relations
			    define viewer: [user, group#member] as self

			type document
			  relations
			    define parent: [folder] as self
			    define viewer: [user] as self or viewer from parent
			`,
			target: DirectRelationReference("document", "viewer"),
			source: DirectRelationReference("user", ""),
			expected: []*RelationshipEdge{
				{
					Type:            DirectEdge,
					TargetReference: DirectRelationReference("document", "viewer"),
					Condition:       NoFurtherEvalCondition,
				},
				{
					Type:            DirectEdge,
					TargetReference: DirectRelationReference("folder", "viewer"),
					Condition:       NoFurtherEvalCondition,
				},
				{
					Type:            DirectEdge,
					TargetReference: DirectRelationReference("group", "member"),
					Condition:       NoFurtherEvalCondition,
				},
			},
		},
		{
			name: "direct_edge_with_union_involving_self_and_computed_userset",
			model: `
			type user

			type group
			  relations
			    define member: [user, group#member] as self

			type document
			  relations
			    define editor: [user, group#member] as self
			    define viewer: [user] as self or editor
			`,
			target: DirectRelationReference("document", "viewer"),
			source: DirectRelationReference("user", ""),
			expected: []*RelationshipEdge{
				{
					Type:            DirectEdge,
					TargetReference: DirectRelationReference("document", "viewer"),
					Condition:       NoFurtherEvalCondition,
				},
				{
					Type:            DirectEdge,
					TargetReference: DirectRelationReference("document", "editor"),
					Condition:       NoFurtherEvalCondition,
				},
				{
					Type:            DirectEdge,
					TargetReference: DirectRelationReference("group", "member"),
					Condition:       NoFurtherEvalCondition,
				},
			},
		},
		{
			name: "circular_reference",
			model: `
			type user

			type team
			  relations
			    define member: [group#member] as self

			type group
			  relations
			    define member: [user, team#member] as self
			`,
			target: DirectRelationReference("team", "member"),
			source: DirectRelationReference("user", ""),
			expected: []*RelationshipEdge{
				{
					Type:            DirectEdge,
					TargetReference: DirectRelationReference("group", "member"),
					Condition:       NoFurtherEvalCondition,
				},
			},
		},
		{
			name: "cyclical_parent/child_definition",
			model: `
			type user

			type folder
			  relations
			    define parent: [folder] as self
			    define viewer: [user] as self or viewer from parent
			`,
			target: DirectRelationReference("folder", "viewer"),
			source: DirectRelationReference("user", ""),
			expected: []*RelationshipEdge{
				{
					Type:            DirectEdge,
					TargetReference: DirectRelationReference("folder", "viewer"),
					Condition:       NoFurtherEvalCondition,
				},
			},
		},
		{
			name: "no_graph_relationship_connectivity",
			model: `
			type user

			type team
			  relations
			    define member: [team#member] as self
			`,
			target:   DirectRelationReference("team", "member"),
			source:   DirectRelationReference("user", ""),
			expected: []*RelationshipEdge{},
		},
		{
			name: "test1",
			model: `
			type user

			type group
			  relations
			    define member: [user] as self

			type folder
			  relations
			    define viewer: [user, group#member] as self

			type document
			  relations
			    define parent: [folder] as self
			    define viewer as viewer from parent
			`,
			target: DirectRelationReference("document", "viewer"),
			source: DirectRelationReference("user", ""),
			expected: []*RelationshipEdge{
				{
					Type:            DirectEdge,
					TargetReference: DirectRelationReference("folder", "viewer"),
					Condition:       NoFurtherEvalCondition,
				},
				{
					Type:            DirectEdge,
					TargetReference: DirectRelationReference("group", "member"),
					Condition:       NoFurtherEvalCondition,
				},
			},
		},
		{
			name: "test2",
			model: `
			type user

			type group
			  relations
			    define member: [user] as self

			type folder
			  relations
			    define viewer: [user, group#member] as self

			type document
			  relations
			    define parent: [folder] as self
			    define viewer as viewer from parent
			`,
			target: DirectRelationReference("document", "viewer"),
			source: DirectRelationReference("group", "member"),
			expected: []*RelationshipEdge{
				{
					Type:            DirectEdge,
					TargetReference: DirectRelationReference("folder", "viewer"),
					Condition:       NoFurtherEvalCondition,
				},
			},
		},
		{
			name: "test3",
			model: `
			type user

			type folder
			  relations
			    define viewer: [user] as self

			type document
			  relations
			    define parent: [folder] as self
			    define viewer as viewer from parent
			`,
			target: DirectRelationReference("document", "viewer"),
			source: DirectRelationReference("folder", "viewer"),
			expected: []*RelationshipEdge{
				{
					Type:             TupleToUsersetEdge,
					TargetReference:  DirectRelationReference("document", "viewer"),
					TuplesetRelation: DirectRelationReference("document", "parent"),
					Condition:        NoFurtherEvalCondition,
				},
			},
		},
		{
			name: "undefined_relation_on_one_type_involved_in_a_ttu",
			model: `
			type user
			type organization

			type folder
			  relations
			    define viewer: [user] as self

			type document
			  relations
			    define parent: [folder, organization] as self
			    define viewer as viewer from parent
			`,
			target: DirectRelationReference("document", "viewer"),
			source: DirectRelationReference("user", ""),
			expected: []*RelationshipEdge{
				{
					Type:            DirectEdge,
					TargetReference: DirectRelationReference("folder", "viewer"),
					Condition:       NoFurtherEvalCondition,
				},
			},
		},
		{
			name: "nested_group_membership_returns_only_top-level_edge",
			model: `
			type user

			type group
			  relations
			    define member: [user, group#member] as self
			`,
			target: DirectRelationReference("group", "member"),
			source: DirectRelationReference("user", ""),
			expected: []*RelationshipEdge{
				{
					Type:            DirectEdge,
					TargetReference: DirectRelationReference("group", "member"),
					Condition:       NoFurtherEvalCondition,
				},
			},
		},
		{
			name: "edges_for_non-assignable_relation",
			model: `
			type organization
			  relations
			    define viewer: [organization] as self
			    define can_view as viewer

			type document
			  relations
			    define parent: [organization] as self
			    define view as can_view from parent
			`,
			target: DirectRelationReference("document", "view"),
			source: DirectRelationReference("organization", ""),
			expected: []*RelationshipEdge{
				{
					Type:            DirectEdge,
					TargetReference: DirectRelationReference("organization", "viewer"),
					Condition:       NoFurtherEvalCondition,
				},
			},
		},
		{
			name: "user_is_a_subset_of_user_*",
			model: `
			type user

			type document
			  relations
			    define viewer: [user:*] as self
			`,
			target: DirectRelationReference("document", "viewer"),
			source: DirectRelationReference("user", ""),
			expected: []*RelationshipEdge{
				{
					Type:            DirectEdge,
					TargetReference: DirectRelationReference("document", "viewer"),
					Condition:       NoFurtherEvalCondition,
				},
			},
		},
		{
			name: "user_*_is_not_a_subset_of_user",
			model: `
			type user

			type document
			  relations
			    define viewer: [user] as self
			`,
			target:   DirectRelationReference("document", "viewer"),
			source:   WildcardRelationReference("user"),
			expected: []*RelationshipEdge{},
		},
		{
			name: "user_*_is_related_to_user_*",
			model: `
			type user

			type document
			  relations
			    define viewer: [user:*] as self
			`,
			target: DirectRelationReference("document", "viewer"),
			source: WildcardRelationReference("user"),
			expected: []*RelationshipEdge{
				{
					Type:            DirectEdge,
					TargetReference: DirectRelationReference("document", "viewer"),
					Condition:       NoFurtherEvalCondition,
				},
			},
		},
		{
			name: "edges_involving_wildcard_in_types",
			model: `
			type user

			type document
			  relations
			    define editor: [user:*] as self
			    define viewer as editor
			`,
			target: DirectRelationReference("document", "viewer"),
			source: DirectRelationReference("user", ""),
			expected: []*RelationshipEdge{
				{
					Type:            DirectEdge,
					TargetReference: DirectRelationReference("document", "editor"),
					Condition:       NoFurtherEvalCondition,
				},
			},
		},
		{
			name: "edges_involving_wildcard_in_source",
			model: `
			type user

			type document
			  relations
			    define editor: [user] as self
			    define viewer as editor
			`,
			target:   DirectRelationReference("document", "viewer"),
			source:   WildcardRelationReference("user"),
			expected: []*RelationshipEdge{},
		},
		{
			name: "edges_involving_wildcards_1",
			model: `
			type user
			type employee
			type group

			type document
			  relations
			    define relation1: [user:*] as self or relation2 or relation3 or relation4
			    define relation2: [group:*] as self
			    define relation3: [employee:*] as self
			    define relation4: [user] as self
			`,
			target: DirectRelationReference("document", "relation1"),
			source: DirectRelationReference("user", ""),
			expected: []*RelationshipEdge{
				{
					Type:            DirectEdge,
					TargetReference: DirectRelationReference("document", "relation1"),
					Condition:       NoFurtherEvalCondition,
				},
				{
					Type:            DirectEdge,
					TargetReference: DirectRelationReference("document", "relation4"),
					Condition:       NoFurtherEvalCondition,
				},
			},
		},
		{
			name: "edges_involving_wildcards_2",
			model: `
			type user

			type document
			  relations
			    define relation1: [user] as self or relation2
			    define relation2: [user:*] as self
			`,
			target: DirectRelationReference("document", "relation1"),
			source: WildcardRelationReference("user"),
			expected: []*RelationshipEdge{
				{
					Type:            DirectEdge,
					TargetReference: DirectRelationReference("document", "relation2"),
					Condition:       NoFurtherEvalCondition,
				},
			},
		},
		{
			name: "indirect_typed_wildcard",
			model: `
			type user

			type group
			  relations
			    define member: [user:*] as self

			type document
			  relations
			    define viewer: [group#member] as self
			`,
			target: DirectRelationReference("document", "viewer"),
			source: DirectRelationReference("user", ""),
			expected: []*RelationshipEdge{
				{
					Type:            DirectEdge,
					TargetReference: DirectRelationReference("group", "member"),
					Condition:       NoFurtherEvalCondition,
				},
			},
		},
		{
			name: "indirect_relationship_multiple_levels_deep",
			model: `
			type user

			type team
			  relations
			    define member: [user] as self

			type group
			  relations
			    define member: [user, team#member] as self

			type document
			  relations
			    define viewer: [user:*, group#member] as self
			`,
			target: DirectRelationReference("document", "viewer"),
			source: DirectRelationReference("user", ""),
			expected: []*RelationshipEdge{
				{
					Type:            DirectEdge,
					TargetReference: DirectRelationReference("document", "viewer"),
					Condition:       NoFurtherEvalCondition,
				},
				{
					Type:            DirectEdge,
					TargetReference: DirectRelationReference("group", "member"),
					Condition:       NoFurtherEvalCondition,
				},
				{
					Type:            DirectEdge,
					TargetReference: DirectRelationReference("team", "member"),
					Condition:       NoFurtherEvalCondition,
				},
			},
		},
		{
			name: "indirect_relationship_multiple_levels_deep_no_connectivity",
			model: `
			type user
			type employee

			type team
			  relations
			    define member: [employee] as self

			type group
			  relations
			    define member: [team#member] as self

			type document
			  relations
			    define viewer: [group#member] as self
			`,
			target:   DirectRelationReference("document", "viewer"),
			source:   DirectRelationReference("user", ""),
			expected: []*RelationshipEdge{},
		},
		{
			name: "edge_through_ttu_on_non-assignable_relation",
			model: `
			type organization
			  relations
			    define viewer: [organization] as self
			    define can_view as viewer

			type document
			  relations
			    define parent: [organization] as self
			    define view as can_view from parent
			`,
			target: DirectRelationReference("document", "view"),
			source: DirectRelationReference("organization", "can_view"),
			expected: []*RelationshipEdge{
				{
					Type:             TupleToUsersetEdge,
					TargetReference:  DirectRelationReference("document", "view"),
					TuplesetRelation: DirectRelationReference("document", "parent"),
					Condition:        NoFurtherEvalCondition,
				},
			},
		},
		{
			name: "indirect_relation_through_ttu_on_non-assignable_relation",
			model: `
			type organization
			  relations
			    define viewer: [organization] as self
			    define can_view as viewer

			type document
			  relations
			    define parent: [organization] as self
			    define view as can_view from parent
			`,
			target: DirectRelationReference("document", "view"),
			source: DirectRelationReference("organization", "viewer"),
			expected: []*RelationshipEdge{
				{
					Type:            ComputedUsersetEdge,
					TargetReference: DirectRelationReference("organization", "can_view"),
					Condition:       NoFurtherEvalCondition,
				},
			},
		},
		{
			name: "ttu_on_non-assignable_relation",
			model: `
			type organization
			  relations
			    define viewer: [organization] as self
			    define can_view as viewer

			type document
			  relations
			    define parent: [organization] as self
			    define view as can_view from parent
			`,
			target: DirectRelationReference("document", "view"),
			source: DirectRelationReference("organization", "can_view"),
			expected: []*RelationshipEdge{
				{
					Type:             TupleToUsersetEdge,
					TargetReference:  DirectRelationReference("document", "view"),
					TuplesetRelation: DirectRelationReference("document", "parent"),
					Condition:        NoFurtherEvalCondition,
				},
			},
		},
		{
			name: "multiple_indirect_non-assignable_relations_through_ttu",
			model: `
			type organization
			  relations
			    define viewer: [organization] as self
			    define view as viewer

			type folder
			  relations
			    define parent: [organization] as self
			    define view as view from parent

			type other

			type document
			  relations
			    define parent: [folder, other] as self
			    define view as view from parent
			`,
			target: DirectRelationReference("document", "view"),
			source: DirectRelationReference("organization", ""),
			expected: []*RelationshipEdge{
				{
					Type:            DirectEdge,
					TargetReference: DirectRelationReference("organization", "viewer"),
					Condition:       NoFurtherEvalCondition,
				},
			},
		},
		{
			name: "multiple_directly_assignable_relationships_through_unions",
			model: `
			type user

			type team
			  relations
			    define admin: [user] as self
			    define member: [user, team#member] as self or admin

			type trial
			  relations
			    define editor: [user, team#member] as self or owner
			    define owner: [user] as self
			    define viewer: [user, team#member] as self or editor
			`,
			target: DirectRelationReference("trial", "viewer"),
			source: DirectRelationReference("user", ""),
			expected: []*RelationshipEdge{
				{
					Type:            DirectEdge,
					TargetReference: DirectRelationReference("trial", "viewer"),
					Condition:       NoFurtherEvalCondition,
				},
				{
					Type:            DirectEdge,
					TargetReference: DirectRelationReference("trial", "editor"),
					Condition:       NoFurtherEvalCondition,
				},
				{
					Type:            DirectEdge,
					TargetReference: DirectRelationReference("trial", "owner"),
					Condition:       NoFurtherEvalCondition,
				},
				{
					Type:            DirectEdge,
					TargetReference: DirectRelationReference("team", "member"),
					Condition:       NoFurtherEvalCondition,
				},
				{
					Type:            DirectEdge,
					TargetReference: DirectRelationReference("team", "admin"),
					Condition:       NoFurtherEvalCondition,
				},
			},
		},
		{
			name: "multiple_assignable_and_non-assignable_computed_usersets",
			model: `
			type user

			type team
			  relations
			    define admin: [user] as self
			    define member: [user, team#member] as self or admin

			type trial
			  relations
			    define editor: [user, team#member] as self or owner
			    define owner: [user] as self
			    define viewer: [user, team#member] as self or editor
			`,
			target: DirectRelationReference("trial", "viewer"),
			source: DirectRelationReference("team", "admin"),
			expected: []*RelationshipEdge{
				{
					Type:            ComputedUsersetEdge,
					TargetReference: DirectRelationReference("team", "member"),
					Condition:       NoFurtherEvalCondition,
				},
			},
		},
		{
			name: "indirect_relationship_through_assignable_computed_userset",
			model: `
			type user

			type team
			  relations
			    define admin: [user] as self
			    define member: [team#member] as self or admin
			`,
			target: DirectRelationReference("team", "member"),
			source: DirectRelationReference("team", "admin"),
			expected: []*RelationshipEdge{
				{
					Type:            ComputedUsersetEdge,
					TargetReference: DirectRelationReference("team", "member"),
					Condition:       NoFurtherEvalCondition,
				},
			},
		},
		{
			name: "indirect_relationship_through_non-assignable_computed_userset",
			model: `
			type user

			type group
			  relations
			    define manager: [user] as self
			    define member as manager

			type document
			  relations
			    define viewer: [group#member] as self
			`,
			target: DirectRelationReference("document", "viewer"),
			source: DirectRelationReference("group", "manager"),
			expected: []*RelationshipEdge{
				{
					Type:            ComputedUsersetEdge,
					TargetReference: DirectRelationReference("group", "member"),
					Condition:       NoFurtherEvalCondition,
				},
			},
		},
		{
			name: "indirect_relationship_through_non-assignable_ttu_1",
			model: `
			type user

			type org
			  relations
			    define dept: [group] as self
			    define dept_member as member from dept

			type group
			  relations
			    define member: [user] as self

			type resource
			  relations
			    define writer: [org#dept_member] as self
			`,
			target: DirectRelationReference("resource", "writer"),
			source: DirectRelationReference("user", ""),
			expected: []*RelationshipEdge{
				{
					Type:            DirectEdge,
					TargetReference: DirectRelationReference("group", "member"),
					Condition:       NoFurtherEvalCondition,
				},
			},
		},
		{
			name: "indirect_relationship_through_non-assignable_ttu_2",
			model: `
			type user

			type org
			  relations
			    define dept: [group] as self
			    define dept_member as member from dept

			type group
			  relations
			    define member: [user] as self

			type resource
			  relations
			    define writer: [org#dept_member] as self
			`,
			target: DirectRelationReference("resource", "writer"),
			source: DirectRelationReference("group", "member"),
			expected: []*RelationshipEdge{
				{
					Type:             TupleToUsersetEdge,
					TargetReference:  DirectRelationReference("org", "dept_member"),
					TuplesetRelation: DirectRelationReference("org", "dept"),
					Condition:        NoFurtherEvalCondition,
				},
			},
		},
		{
			name: "indirect_relationship_through_non-assignable_ttu_3",
			model: `
			type user

			type org
			  relations
			    define dept: [group] as self
			    define dept_member as member from dept

			type group
			  relations
			    define member: [user] as self

			type resource
			  relations
			    define writer: [org#dept_member] as self
			`,
			target: DirectRelationReference("resource", "writer"),
			source: DirectRelationReference("org", "dept_member"),
			expected: []*RelationshipEdge{
				{
					Type:            DirectEdge,
					TargetReference: DirectRelationReference("resource", "writer"),
					Condition:       NoFurtherEvalCondition,
				},
			},
		},
		{
			name: "unrelated_source_and_target_relationship_involving_ttu",
			model: `
			type user

			type folder
				relations
					define viewer: [user] as self

			type document
				relations
					define can_read as viewer from parent
					define parent: [document,folder] as self
					define viewer: [user] as self
			`,
			target:   DirectRelationReference("document", "can_read"),
			source:   DirectRelationReference("document", ""),
			expected: []*RelationshipEdge{},
		},
		{
			name: "simple_computeduserset_indirect_ref",
			model: `
			type user

			type document
			  relations
			    define parent: [document] as self
			    define viewer: [user] as self or viewer from parent
				define can_view as viewer
			`,
			target: DirectRelationReference("document", "can_view"),
			source: DirectRelationReference("document", "viewer"),
			expected: []*RelationshipEdge{
				{
					Type:            ComputedUsersetEdge,
					TargetReference: DirectRelationReference("document", "can_view"),
					Condition:       NoFurtherEvalCondition,
				},
				{
					Type:             TupleToUsersetEdge,
					TargetReference:  DirectRelationReference("document", "viewer"),
					TuplesetRelation: DirectRelationReference("document", "parent"),
					Condition:        NoFurtherEvalCondition,
				},
			},
		},
		{
			name: "follow_computed_relation_of_ttu_to_computed_userset",
			model: `
			type user
			type folder
			  relations
				define owner: [user] as self
				define viewer: [user] as self or owner
			type document
			  relations
				define can_read as viewer from parent
				define parent: [document, folder] as self
				define viewer: [user] as self
			`,
			target: DirectRelationReference("document", "can_read"),
			source: DirectRelationReference("folder", "owner"),
			expected: []*RelationshipEdge{
				{
					Type:            ComputedUsersetEdge,
					TargetReference: DirectRelationReference("folder", "viewer"),
					Condition:       NoFurtherEvalCondition,
				},
			},
		},
		{
			name: "computed_target_of_ttu_related_to_same_type",
			model: `
			type folder
			  relations
				define viewer: [folder] as self

			type document
			  relations
				define parent: [folder] as self
				define viewer as viewer from parent
			`,
			target: DirectRelationReference("document", "viewer"),
			source: DirectRelationReference("folder", "viewer"),
			expected: []*RelationshipEdge{
				{
					Type:             TupleToUsersetEdge,
					TargetReference:  DirectRelationReference("document", "viewer"),
					TuplesetRelation: DirectRelationReference("document", "parent"),
					Condition:        NoFurtherEvalCondition,
				},
			},
		},
		{
			name: "basic_relation_with_intersection_1",
			model: `
			type user

			type document
			  relations
				define allowed: [user] as self
				define viewer: [user] as self and allowed
			`,
			target: DirectRelationReference("document", "viewer"),
			source: DirectRelationReference("user", ""),
			expected: []*RelationshipEdge{
				{
					Type:            DirectEdge,
					TargetReference: DirectRelationReference("document", "viewer"),
					Condition:       RequiresFurtherEvalCondition,
				},
				{
					Type:            DirectEdge,
					TargetReference: DirectRelationReference("document", "allowed"),
					Condition:       NoFurtherEvalCondition,
				},
			},
		},
		{
			name: "basic_relation_with_intersection_2",
			model: `
			type user

			type document
			  relations
				define allowed: [user] as self
				define editor: [user] as self
				define viewer as editor and allowed
			`,
			target: DirectRelationReference("document", "viewer"),
			source: DirectRelationReference("user", ""),
			expected: []*RelationshipEdge{
				{
					Type:            DirectEdge,
					TargetReference: DirectRelationReference("document", "editor"),
					Condition:       RequiresFurtherEvalCondition,
				},
				{
					Type:            DirectEdge,
					TargetReference: DirectRelationReference("document", "allowed"),
					Condition:       NoFurtherEvalCondition,
				},
			},
		},
		{
			name: "basic_relation_with_intersection_3",
			model: `
			type user

			type document
			  relations
				define allowed: [user] as self
				define editor: [user] as self
				define viewer: [user] as allowed and self
			`,
			target: DirectRelationReference("document", "viewer"),
			source: DirectRelationReference("user", ""),
			expected: []*RelationshipEdge{
				{
					Type:            DirectEdge,
					TargetReference: DirectRelationReference("document", "allowed"),
					Condition:       RequiresFurtherEvalCondition,
				},
				{
					Type:            DirectEdge,
					TargetReference: DirectRelationReference("document", "viewer"),
					Condition:       NoFurtherEvalCondition,
				},
			},
		},
		{
			name: "basic_relation_with_exclusion_1",
			model: `
			type user

			type document
			  relations
				define restricted: [user] as self
				define viewer: [user] as self but not restricted
			`,
			target: DirectRelationReference("document", "viewer"),
			source: DirectRelationReference("user", ""),
			expected: []*RelationshipEdge{
				{
					Type:            DirectEdge,
					TargetReference: DirectRelationReference("document", "viewer"),
					Condition:       RequiresFurtherEvalCondition,
				},
				{
					Type:            DirectEdge,
					TargetReference: DirectRelationReference("document", "restricted"),
					Condition:       NoFurtherEvalCondition,
				},
			},
		},
		{
			name: "basic_relation_with_exclusion_2",
			model: `
			type user

			type document
			  relations
				define restricted: [user] as self
				define editor: [user] as self
				define viewer as editor but not restricted
			`,
			target: DirectRelationReference("document", "viewer"),
			source: DirectRelationReference("user", ""),
			expected: []*RelationshipEdge{
				{
					Type:            DirectEdge,
					TargetReference: DirectRelationReference("document", "editor"),
					Condition:       RequiresFurtherEvalCondition,
				},
				{
					Type:            DirectEdge,
					TargetReference: DirectRelationReference("document", "restricted"),
					Condition:       NoFurtherEvalCondition,
				},
			},
		},
		{
			name: "basic_relation_with_exclusion_3",
			model: `
			type user

			type document
			  relations
				define allowed: [user] as self
				define viewer: [user] as allowed but not self
			`,
			target: DirectRelationReference("document", "viewer"),
			source: DirectRelationReference("user", ""),
			expected: []*RelationshipEdge{
				{
					Type:            DirectEdge,
					TargetReference: DirectRelationReference("document", "allowed"),
					Condition:       RequiresFurtherEvalCondition,
				},
				{
					Type:            DirectEdge,
					TargetReference: DirectRelationReference("document", "viewer"),
					Condition:       NoFurtherEvalCondition,
				},
			},
		},
		{
			name: "ttu_through_direct_rewrite_1",
			model: `
			type folder
				relations
				define viewer: [folder] as self
		
			type document
				relations
				define parent: [folder] as self
				define viewer as viewer from parent
			`,
			target: DirectRelationReference("document", "viewer"),
			source: DirectRelationReference("folder", "viewer"),
			expected: []*RelationshipEdge{
				{
					Type:             TupleToUsersetEdge,
					TargetReference:  DirectRelationReference("document", "viewer"),
					TuplesetRelation: DirectRelationReference("document", "parent"),
					Condition:        NoFurtherEvalCondition,
				},
			},
		},
		{
			name: "ttu_through_direct_rewrite_2",
			model: `
			type folder
				relations
				define viewer: [folder] as self
		
			type document
				relations
				define parent: [folder] as self
				define viewer as viewer from parent
			`,
			target: DirectRelationReference("document", "viewer"),
			source: DirectRelationReference("folder", ""),
			expected: []*RelationshipEdge{
				{
					Type:            DirectEdge,
					TargetReference: DirectRelationReference("folder", "viewer"),
					Condition:       NoFurtherEvalCondition,
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			typedefs := parser.MustParse(test.model)
			typesys := New(&openfgav1.AuthorizationModel{
				SchemaVersion:   SchemaVersion1_1,
				TypeDefinitions: typedefs,
			})

			g := NewGraph(typesys)

			edges, err := g.GetRelationshipEdges(test.target, test.source)
			require.NoError(t, err)

			cmpOpts := []cmp.Option{
				cmpopts.IgnoreUnexported(openfgav1.RelationReference{}),
				RelationshipEdgeTransformer,
			}
			if diff := cmp.Diff(test.expected, edges, cmpOpts...); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
