package migrations

import (
	"github.com/sqlbunny/sqlbunny/runtime/migration"
	"github.com/sqlbunny/sqlbunny/sqlschema/operations"
)

func init() {
	Store.Register(&migration.Migration{
		Name:         "00001_c90fc2",
		Dependencies: nil,
		Operations: []operations.Operation{
			operations.CreateTable{
				SchemaName: "",
				TableName:  "pull_request",
				Columns: []operations.Column{
					operations.Column{
						Name:     "author",
						Type:     "text",
						Default:  "''",
						Nullable: false,
					},
					operations.Column{
						Name:     "ci_check_due_at",
						Type:     "timestamptz",
						Default:  "",
						Nullable: true,
					},
					operations.Column{
						Name:     "ci_notified_at",
						Type:     "timestamptz",
						Default:  "",
						Nullable: true,
					},
					operations.Column{
						Name:     "ci_state",
						Type:     "integer",
						Default:  "0",
						Nullable: false,
					},
					operations.Column{
						Name:     "created_at",
						Type:     "timestamptz",
						Default:  "'0001-01-01 00:00:00+00'",
						Nullable: false,
					},
					operations.Column{
						Name:     "draft_notified_at",
						Type:     "timestamptz",
						Default:  "",
						Nullable: true,
					},
					operations.Column{
						Name:     "first_reviewable_at",
						Type:     "timestamptz",
						Default:  "",
						Nullable: true,
					},
					operations.Column{
						Name:     "head_sha",
						Type:     "text",
						Default:  "''",
						Nullable: false,
					},
					operations.Column{
						Name:     "html_url",
						Type:     "text",
						Default:  "''",
						Nullable: false,
					},
					operations.Column{
						Name:     "is_draft",
						Type:     "boolean",
						Default:  "false",
						Nullable: false,
					},
					operations.Column{
						Name:     "is_reviewable",
						Type:     "boolean",
						Default:  "false",
						Nullable: false,
					},
					operations.Column{
						Name:     "number",
						Type:     "bigint",
						Default:  "0",
						Nullable: false,
					},
					operations.Column{
						Name:     "repo_id",
						Type:     "bigint",
						Default:  "0",
						Nullable: false,
					},
					operations.Column{
						Name:     "state",
						Type:     "integer",
						Default:  "0",
						Nullable: false,
					},
					operations.Column{
						Name:     "title",
						Type:     "text",
						Default:  "''",
						Nullable: false,
					},
					operations.Column{
						Name:     "updated_at",
						Type:     "timestamptz",
						Default:  "'0001-01-01 00:00:00+00'",
						Nullable: false,
					},
					operations.Column{
						Name:     "welcomed_at",
						Type:     "timestamptz",
						Default:  "",
						Nullable: true,
					},
				},
			},
			operations.CreateTable{
				SchemaName: "",
				TableName:  "repo",
				Columns: []operations.Column{
					operations.Column{
						Name:     "id",
						Type:     "bigint",
						Default:  "0",
						Nullable: false,
					},
					operations.Column{
						Name:     "installation_id",
						Type:     "bigint",
						Default:  "0",
						Nullable: false,
					},
					operations.Column{
						Name:     "name",
						Type:     "text",
						Default:  "''",
						Nullable: false,
					},
					operations.Column{
						Name:     "owner",
						Type:     "text",
						Default:  "''",
						Nullable: false,
					},
				},
			},
			operations.CreateIndex{
				SchemaName: "",
				TableName:  "pull_request",
				IndexName:  "pull_request___ci_check_due_at___idx",
				Columns: []string{
					"ci_check_due_at",
				},
				Method: "",
				Where:  "",
				Unique: false,
			},
			operations.CreateIndex{
				SchemaName: "",
				TableName:  "pull_request",
				IndexName:  "pull_request___head_sha___idx",
				Columns: []string{
					"head_sha",
				},
				Method: "",
				Where:  "",
				Unique: false,
			},
			operations.CreateIndex{
				SchemaName: "",
				TableName:  "pull_request",
				IndexName:  "pull_request___is_reviewable___first_reviewable_at___idx",
				Columns: []string{
					"is_reviewable",
					"first_reviewable_at",
				},
				Method: "",
				Where:  "",
				Unique: false,
			},
			operations.AlterTable{
				SchemaName: "",
				TableName:  "pull_request",
				Ops: []operations.AlterTableSuboperation{
					operations.AlterTableCreatePrimaryKey{
						Columns: []string{
							"repo_id",
							"number",
						},
					},
				},
			},
			operations.AlterTable{
				SchemaName: "",
				TableName:  "repo",
				Ops: []operations.AlterTableSuboperation{
					operations.AlterTableCreatePrimaryKey{
						Columns: []string{
							"id",
						},
					},
				},
			},
			operations.AlterTable{
				SchemaName: "",
				TableName:  "pull_request",
				Ops: []operations.AlterTableSuboperation{
					operations.AlterTableCreateForeignKey{
						Name: "pull_request___repo_id___fkey",
						Columns: []string{
							"repo_id",
						},
						ForeignSchema: "",
						ForeignTable:  "repo",
						ForeignColumns: []string{
							"id",
						},
						NotValid: false,
					},
				},
			},
		},
		Transaction: false,
	})
}
