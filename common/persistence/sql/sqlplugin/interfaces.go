// The MIT License
//
// Copyright (c) 2020 Temporal Technologies Inc.  All rights reserved.
//
// Copyright (c) 2020 Uber Technologies, Inc.
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

package sqlplugin

import (
	"context"
	"database/sql"

	"github.com/jmoiron/sqlx"
	"go.temporal.io/server/common/config"
	"go.temporal.io/server/common/log"
	"go.temporal.io/server/common/metrics"
	"go.temporal.io/server/common/resolver"
)

type (
	DbKind int
)

const (
	DbKindUnknown DbKind = iota
	DbKindMain
	DbKindVisibility
)

type VersionedBlob struct {
	Version      int64
	Data         []byte
	DataEncoding string
}

type (
	// Plugin defines the interface for any SQL database that needs to implement
	Plugin interface {
		CreateDB(dbKind DbKind, cfg *config.SQL, r resolver.ServiceResolver, l log.Logger, mh metrics.Handler) (GenericDB, error)
	}

	// TableCRUD defines the API for interacting with the database tables
	TableCRUD interface {
		ClusterMetadata
		Namespace
		Visibility
		QueueMessage
		QueueMetadata
		QueueV2Message
		QueueV2Metadata

		MatchingTask
		MatchingTaskQueue

		NexusEndpoints

		HistoryNode
		HistoryTree

		HistoryShard

		HistoryExecution
		HistoryExecutionBuffer
		HistoryExecutionActivity
		HistoryExecutionChildWorkflow
		HistoryExecutionTimer
		HistoryExecutionRequestCancel
		HistoryExecutionSignal
		HistoryExecutionSignalRequest
		HistoryExecutionChasm

		HistoryImmediateTask
		HistoryScheduledTask
		HistoryTransferTask
		HistoryTimerTask
		HistoryReplicationTask
		HistoryReplicationDLQTask
		HistoryVisibilityTask
	}

	// AdminCRUD defines admin operations for CLI and test suites
	AdminCRUD interface {
		CreateSchemaVersionTables() error
		ReadSchemaVersion(database string) (string, error)
		UpdateSchemaVersion(database string, newVersion string, minCompatibleVersion string) error
		WriteSchemaUpdateLog(oldVersion string, newVersion string, manifestMD5 string, desc string) error
		ListTables(database string) ([]string, error)
		DropTable(table string) error
		DropAllTables(database string) error
		CreateDatabase(database string) error
		DropDatabase(database string) error
		Exec(stmt string, args ...interface{}) error
	}

	// Tx defines the API for a SQL transaction
	Tx interface {
		TableCRUD
		Commit() error
		Rollback() error
	}

	// DB defines the API for regular SQL operations of a Temporal server
	DB interface {
		TableCRUD
		GenericDB
		BeginTx(ctx context.Context) (Tx, error)
		IsDupEntryError(err error) bool
	}

	// AdminDB defines the API for admin SQL operations for CLI and testing suites
	AdminDB interface {
		AdminCRUD
		GenericDB
		ExpectedVersion() string
		VerifyVersion() error
	}

	GenericDB interface {
		DbName() string
		PluginName() string
		Close() error
	}

	// Conn defines the API for a single database connection
	Conn interface {
		Rebind(query string) string
		ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
		NamedExecContext(ctx context.Context, query string, arg interface{}) (sql.Result, error)
		GetContext(ctx context.Context, dest interface{}, query string, args ...interface{}) error
		SelectContext(ctx context.Context, dest interface{}, query string, args ...interface{}) error
		PrepareNamedContext(ctx context.Context, query string) (*sqlx.NamedStmt, error)
	}
)

func (k DbKind) String() string {
	switch k {
	case DbKindMain:
		return "main"
	case DbKindVisibility:
		return "visibility"
	default:
		return "unknown"
	}
}
