package migrations

import (
	"github.com/sqlbunny/sqlbunny/runtime/migration"
	"github.com/sqlbunny/sqlbunny/sqlschema/operations"
)

func init() {
	Store.Register(&migration.Migration{
		Name: "00002_43f374",
		Dependencies: []string{
			"00001_c90fc2",
		},
		Operations: []operations.Operation{
			operations.AlterTable{
				SchemaName: "",
				TableName:  "pull_request",
				Ops: []operations.AlterTableSuboperation{
					operations.AlterTableAddColumn{
						Name:     "labels",
						Type:     "text[]",
						Default:  "'{}'",
						Nullable: false,
					},
				},
			},
			operations.CreateTable{
				SchemaName: "",
				TableName:  "label",
				Columns: []operations.Column{
					operations.Column{
						Name:     "color",
						Type:     "text",
						Default:  "''",
						Nullable: false,
					},
					operations.Column{
						Name:     "name",
						Type:     "text",
						Default:  "''",
						Nullable: false,
					},
					operations.Column{
						Name:     "repo_id",
						Type:     "bigint",
						Default:  "0",
						Nullable: false,
					},
				},
			},
			operations.AlterTable{
				SchemaName: "",
				TableName:  "label",
				Ops: []operations.AlterTableSuboperation{
					operations.AlterTableCreatePrimaryKey{
						Columns: []string{
							"repo_id",
							"name",
						},
					},
				},
			},
			operations.AlterTable{
				SchemaName: "",
				TableName:  "label",
				Ops: []operations.AlterTableSuboperation{
					operations.AlterTableCreateForeignKey{
						Name: "label___repo_id___fkey",
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
