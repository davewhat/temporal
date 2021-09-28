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

package sql

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"go.temporal.io/api/serviceerror"

	persistencespb "go.temporal.io/server/api/persistence/v1"
	"go.temporal.io/server/common/collection"
	"go.temporal.io/server/common/persistence"
	p "go.temporal.io/server/common/persistence"
	"go.temporal.io/server/common/persistence/serialization"
	"go.temporal.io/server/common/persistence/sql/sqlplugin"
)

func (m *sqlExecutionStore) AddTasks(
	request *p.InternalAddTasksRequest,
) error {
	ctx, cancel := newExecutionContext()
	defer cancel()
	return m.txExecuteShardLocked(ctx,
		"AddTasks",
		request.ShardID,
		request.RangeID,
		func(tx sqlplugin.Tx) error {
			return applyTasks(ctx,
				tx,
				request.ShardID,
				request.TransferTasks,
				request.TimerTasks,
				request.ReplicationTasks,
				request.VisibilityTasks,
			)
		})
}

func (m *sqlExecutionStore) GetTransferTask(
	request *persistence.GetTransferTaskRequest,
) (*persistence.GetTransferTaskResponse, error) {
	ctx, cancel := newExecutionContext()
	defer cancel()
	rows, err := m.Db.SelectFromTransferTasks(ctx, sqlplugin.TransferTasksFilter{
		ShardID: request.ShardID,
		TaskID:  request.TaskID,
	})
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, serviceerror.NewNotFound(fmt.Sprintf("GetTransferTask operation failed. Task with ID %v not found. Error: %v", request.TaskID, err))
		}
		return nil, serviceerror.NewUnavailable(fmt.Sprintf("GetTransferTask operation failed. Failed to get record. TaskId: %v. Error: %v", request.TaskID, err))
	}

	if len(rows) == 0 {
		return nil, serviceerror.NewUnavailable(fmt.Sprintf("GetTransferTask operation failed. Failed to get record. TaskId: %v", request.TaskID))
	}

	transferRow := rows[0]
	transferInfo, err := serialization.TransferTaskInfoFromBlob(transferRow.Data, transferRow.DataEncoding)
	if err != nil {
		return nil, err
	}

	resp := &persistence.GetTransferTaskResponse{TransferTaskInfo: transferInfo}

	return resp, nil
}

func (m *sqlExecutionStore) GetTransferTasks(
	request *p.GetTransferTasksRequest,
) (*p.GetTransferTasksResponse, error) {
	ctx, cancel := newExecutionContext()
	defer cancel()
	rows, err := m.Db.RangeSelectFromTransferTasks(ctx, sqlplugin.TransferTasksRangeFilter{
		ShardID:   request.ShardID,
		MinTaskID: request.ReadLevel,
		MaxTaskID: request.MaxReadLevel,
	})
	if err != nil {
		if err != sql.ErrNoRows {
			return nil, serviceerror.NewUnavailable(fmt.Sprintf("GetTransferTasks operation failed. Select failed. Error: %v", err))
		}
	}
	resp := &p.GetTransferTasksResponse{Tasks: make([]*persistencespb.TransferTaskInfo, len(rows))}
	for i, row := range rows {
		info, err := serialization.TransferTaskInfoFromBlob(row.Data, row.DataEncoding)
		if err != nil {
			return nil, err
		}
		resp.Tasks[i] = info
	}

	return resp, nil
}

func (m *sqlExecutionStore) CompleteTransferTask(
	request *p.CompleteTransferTaskRequest,
) error {
	ctx, cancel := newExecutionContext()
	defer cancel()
	if _, err := m.Db.DeleteFromTransferTasks(ctx, sqlplugin.TransferTasksFilter{
		ShardID: request.ShardID,
		TaskID:  request.TaskID,
	}); err != nil {
		return serviceerror.NewUnavailable(fmt.Sprintf("CompleteTransferTask operation failed. Error: %v", err))
	}
	return nil
}

func (m *sqlExecutionStore) RangeCompleteTransferTask(
	request *p.RangeCompleteTransferTaskRequest,
) error {
	ctx, cancel := newExecutionContext()
	defer cancel()
	if _, err := m.Db.RangeDeleteFromTransferTasks(ctx, sqlplugin.TransferTasksRangeFilter{
		ShardID:   request.ShardID,
		MinTaskID: request.ExclusiveBeginTaskID,
		MaxTaskID: request.InclusiveEndTaskID,
	}); err != nil {
		return serviceerror.NewUnavailable(fmt.Sprintf("RangeCompleteTransferTask operation failed. Error: %v", err))
	}
	return nil
}

func (m *sqlExecutionStore) GetTimerTask(
	request *persistence.GetTimerTaskRequest,
) (*persistence.GetTimerTaskResponse, error) {
	ctx, cancel := newExecutionContext()
	defer cancel()
	rows, err := m.Db.SelectFromTimerTasks(ctx, sqlplugin.TimerTasksFilter{
		ShardID:             request.ShardID,
		TaskID:              request.TaskID,
		VisibilityTimestamp: request.VisibilityTimestamp,
	})
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, serviceerror.NewNotFound(fmt.Sprintf("GetTimerTask operation failed. Task with ID %v not found. Error: %v", request.TaskID, err))
		}
		return nil, serviceerror.NewUnavailable(fmt.Sprintf("GetTimerTask operation failed. Failed to get record. TaskId: %v. Error: %v", request.TaskID, err))
	}

	if len(rows) == 0 {
		return nil, serviceerror.NewUnavailable(fmt.Sprintf("GetTimerTask operation failed. Failed to get record. TaskId: %v", request.TaskID))
	}

	timerRow := rows[0]
	timerInfo, err := serialization.TimerTaskInfoFromBlob(timerRow.Data, timerRow.DataEncoding)
	if err != nil {
		return nil, err
	}

	resp := &persistence.GetTimerTaskResponse{TimerTaskInfo: timerInfo}

	return resp, nil
}

func (m *sqlExecutionStore) GetTimerIndexTasks(
	request *p.GetTimerIndexTasksRequest,
) (*p.GetTimerIndexTasksResponse, error) {
	ctx, cancel := newExecutionContext()
	defer cancel()
	pageToken := &timerTaskPageToken{TaskID: math.MinInt64, Timestamp: request.MinTimestamp}
	if len(request.NextPageToken) > 0 {
		if err := pageToken.deserialize(request.NextPageToken); err != nil {
			return nil, serviceerror.NewInternal(fmt.Sprintf("error deserializing timerTaskPageToken: %v", err))
		}
	}

	rows, err := m.Db.RangeSelectFromTimerTasks(ctx, sqlplugin.TimerTasksRangeFilter{
		ShardID:                request.ShardID,
		MinVisibilityTimestamp: pageToken.Timestamp,
		TaskID:                 pageToken.TaskID,
		MaxVisibilityTimestamp: request.MaxTimestamp,
		PageSize:               request.BatchSize + 1,
	})

	if err != nil && err != sql.ErrNoRows {
		return nil, serviceerror.NewUnavailable(fmt.Sprintf("GetTimerTasks operation failed. Select failed. Error: %v", err))
	}

	resp := &p.GetTimerIndexTasksResponse{Timers: make([]*persistencespb.TimerTaskInfo, len(rows))}
	for i, row := range rows {
		info, err := serialization.TimerTaskInfoFromBlob(row.Data, row.DataEncoding)
		if err != nil {
			return nil, err
		}
		resp.Timers[i] = info
	}

	if len(resp.Timers) > request.BatchSize {
		goVisibilityTimestamp := resp.Timers[request.BatchSize].VisibilityTime
		if goVisibilityTimestamp == nil {
			return nil, serviceerror.NewInternal(fmt.Sprintf("GetTimerTasks: time for page token is nil - TaskId '%v'", resp.Timers[request.BatchSize].TaskId))
		}

		pageToken = &timerTaskPageToken{
			TaskID:    resp.Timers[request.BatchSize].GetTaskId(),
			Timestamp: *goVisibilityTimestamp,
		}
		resp.Timers = resp.Timers[:request.BatchSize]
		nextToken, err := pageToken.serialize()
		if err != nil {
			return nil, serviceerror.NewInternal(fmt.Sprintf("GetTimerTasks: error serializing page token: %v", err))
		}
		resp.NextPageToken = nextToken
	}

	return resp, nil
}

func (m *sqlExecutionStore) CompleteTimerTask(
	request *p.CompleteTimerTaskRequest,
) error {
	ctx, cancel := newExecutionContext()
	defer cancel()
	if _, err := m.Db.DeleteFromTimerTasks(ctx, sqlplugin.TimerTasksFilter{
		ShardID:             request.ShardID,
		VisibilityTimestamp: request.VisibilityTimestamp,
		TaskID:              request.TaskID,
	}); err != nil {
		return serviceerror.NewUnavailable(fmt.Sprintf("CompleteTimerTask operation failed. Error: %v", err))
	}
	return nil
}

func (m *sqlExecutionStore) RangeCompleteTimerTask(
	request *p.RangeCompleteTimerTaskRequest,
) error {
	ctx, cancel := newExecutionContext()
	defer cancel()
	start := request.InclusiveBeginTimestamp
	end := request.ExclusiveEndTimestamp
	if _, err := m.Db.RangeDeleteFromTimerTasks(ctx, sqlplugin.TimerTasksRangeFilter{
		ShardID:                request.ShardID,
		MinVisibilityTimestamp: start,
		MaxVisibilityTimestamp: end,
	}); err != nil {
		return serviceerror.NewUnavailable(fmt.Sprintf("CompleteTimerTask operation failed. Error: %v", err))
	}
	return nil
}

func (m *sqlExecutionStore) GetReplicationTask(
	request *persistence.GetReplicationTaskRequest,
) (*persistence.GetReplicationTaskResponse, error) {
	ctx, cancel := newExecutionContext()
	defer cancel()
	rows, err := m.Db.SelectFromReplicationTasks(ctx, sqlplugin.ReplicationTasksFilter{
		ShardID: request.ShardID,
		TaskID:  request.TaskID,
	})
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, serviceerror.NewNotFound(fmt.Sprintf("GetReplicationTask operation failed. Task with ID %v not found. Error: %v", request.TaskID, err))
		}
		return nil, serviceerror.NewUnavailable(fmt.Sprintf("GetReplicationTask operation failed. Failed to get record. TaskId: %v. Error: %v", request.TaskID, err))
	}

	if len(rows) == 0 {
		return nil, serviceerror.NewUnavailable(fmt.Sprintf("GetReplicationTask operation failed. Failed to get record. TaskId: %v", request.TaskID))
	}

	replicationRow := rows[0]
	replicationInfo, err := serialization.ReplicationTaskInfoFromBlob(replicationRow.Data, replicationRow.DataEncoding)
	if err != nil {
		return nil, err
	}

	resp := &persistence.GetReplicationTaskResponse{ReplicationTaskInfo: replicationInfo}

	return resp, nil
}

func (m *sqlExecutionStore) GetReplicationTasks(
	request *p.GetReplicationTasksRequest,
) (*p.GetReplicationTasksResponse, error) {
	ctx, cancel := newExecutionContext()
	defer cancel()
	readLevel, maxReadLevelInclusive, err := getReadLevels(request)
	if err != nil {
		return nil, err
	}

	rows, err := m.Db.RangeSelectFromReplicationTasks(ctx, sqlplugin.ReplicationTasksRangeFilter{
		ShardID:   request.ShardID,
		MinTaskID: readLevel,
		MaxTaskID: maxReadLevelInclusive,
		PageSize:  request.BatchSize,
	})

	switch err {
	case nil:
		return m.populateGetReplicationTasksResponse(rows, request.MaxTaskID)
	case sql.ErrNoRows:
		return &p.GetReplicationTasksResponse{}, nil
	default:
		return nil, serviceerror.NewUnavailable(fmt.Sprintf("GetReplicationTasks operation failed. Select failed: %v", err))
	}
}

func getReadLevels(
	request *p.GetReplicationTasksRequest,
) (readLevel int64, maxReadLevelInclusive int64, err error) {
	readLevel = request.MinTaskID
	if len(request.NextPageToken) > 0 {
		readLevel, err = deserializePageToken(request.NextPageToken)
		if err != nil {
			return 0, 0, err
		}
	}

	maxReadLevelInclusive = collection.MaxInt64(readLevel+int64(request.BatchSize), request.MaxTaskID)
	return readLevel, maxReadLevelInclusive, nil
}

func (m *sqlExecutionStore) populateGetReplicationTasksResponse(
	rows []sqlplugin.ReplicationTasksRow,
	requestMaxReadLevel int64,
) (*p.GetReplicationTasksResponse, error) {
	if len(rows) == 0 {
		return &p.GetReplicationTasksResponse{}, nil
	}

	var tasks = make([]*persistencespb.ReplicationTaskInfo, len(rows))
	for i, row := range rows {
		info, err := serialization.ReplicationTaskInfoFromBlob(row.Data, row.DataEncoding)
		if err != nil {
			return nil, err
		}

		tasks[i] = info
	}
	var nextPageToken []byte
	lastTaskID := rows[len(rows)-1].TaskID
	if lastTaskID < requestMaxReadLevel {
		nextPageToken = serializePageToken(lastTaskID)
	}
	return &p.GetReplicationTasksResponse{
		Tasks:         tasks,
		NextPageToken: nextPageToken,
	}, nil
}

func (m *sqlExecutionStore) populateGetReplicationDLQTasksResponse(
	rows []sqlplugin.ReplicationDLQTasksRow,
	requestMaxReadLevel int64,
) (*p.GetReplicationTasksResponse, error) {
	if len(rows) == 0 {
		return &p.GetReplicationTasksResponse{}, nil
	}

	var tasks = make([]*persistencespb.ReplicationTaskInfo, len(rows))
	for i, row := range rows {
		info, err := serialization.ReplicationTaskInfoFromBlob(row.Data, row.DataEncoding)
		if err != nil {
			return nil, err
		}

		tasks[i] = info
	}
	var nextPageToken []byte
	lastTaskID := rows[len(rows)-1].TaskID
	if lastTaskID < requestMaxReadLevel {
		nextPageToken = serializePageToken(lastTaskID)
	}
	return &p.GetReplicationTasksResponse{
		Tasks:         tasks,
		NextPageToken: nextPageToken,
	}, nil
}

func (m *sqlExecutionStore) CompleteReplicationTask(
	request *p.CompleteReplicationTaskRequest,
) error {
	ctx, cancel := newExecutionContext()
	defer cancel()
	if _, err := m.Db.DeleteFromReplicationTasks(ctx, sqlplugin.ReplicationTasksFilter{
		ShardID: request.ShardID,
		TaskID:  request.TaskID,
	}); err != nil {
		return serviceerror.NewUnavailable(fmt.Sprintf("CompleteReplicationTask operation failed. Error: %v", err))
	}
	return nil
}

func (m *sqlExecutionStore) RangeCompleteReplicationTask(
	request *p.RangeCompleteReplicationTaskRequest,
) error {
	ctx, cancel := newExecutionContext()
	defer cancel()
	if _, err := m.Db.RangeDeleteFromReplicationTasks(ctx, sqlplugin.ReplicationTasksRangeFilter{
		ShardID:   request.ShardID,
		MinTaskID: 0,
		MaxTaskID: request.InclusiveEndTaskID,
	}); err != nil {
		return serviceerror.NewUnavailable(fmt.Sprintf("RangeCompleteReplicationTask operation failed. Error: %v", err))
	}
	return nil
}

func (m *sqlExecutionStore) PutReplicationTaskToDLQ(
	request *p.PutReplicationTaskToDLQRequest,
) error {
	ctx, cancel := newExecutionContext()
	defer cancel()
	replicationTask := request.TaskInfo
	blob, err := serialization.ReplicationTaskInfoToBlob(replicationTask)

	if err != nil {
		return err
	}

	_, err = m.Db.InsertIntoReplicationDLQTasks(ctx, []sqlplugin.ReplicationDLQTasksRow{{
		SourceClusterName: request.SourceClusterName,
		ShardID:           request.ShardID,
		TaskID:            replicationTask.GetTaskId(),
		Data:              blob.Data,
		DataEncoding:      blob.EncodingType.String(),
	}})

	// Tasks are immutable. So it's fine if we already persisted it before.
	// This can happen when tasks are retried (ack and cleanup can have lag on source side).
	if err != nil && !m.Db.IsDupEntryError(err) {
		return serviceerror.NewUnavailable(fmt.Sprintf("Failed to create replication tasks. Error: %v", err))
	}

	return nil
}

func (m *sqlExecutionStore) GetReplicationTasksFromDLQ(
	request *p.GetReplicationTasksFromDLQRequest,
) (*p.GetReplicationTasksFromDLQResponse, error) {
	ctx, cancel := newExecutionContext()
	defer cancel()
	readLevel, maxReadLevelInclusive, err := getReadLevels(&request.GetReplicationTasksRequest)
	if err != nil {
		return nil, err
	}

	rows, err := m.Db.RangeSelectFromReplicationDLQTasks(ctx, sqlplugin.ReplicationDLQTasksRangeFilter{
		ShardID:           request.ShardID,
		MinTaskID:         readLevel,
		MaxTaskID:         maxReadLevelInclusive,
		PageSize:          request.BatchSize,
		SourceClusterName: request.SourceClusterName,
	})

	switch err {
	case nil:
		return m.populateGetReplicationDLQTasksResponse(rows, request.MaxTaskID)
	case sql.ErrNoRows:
		return &p.GetReplicationTasksResponse{}, nil
	default:
		return nil, serviceerror.NewUnavailable(fmt.Sprintf("GetReplicationTasks operation failed. Select failed: %v", err))
	}
}

func (m *sqlExecutionStore) DeleteReplicationTaskFromDLQ(
	request *p.DeleteReplicationTaskFromDLQRequest,
) error {
	ctx, cancel := newExecutionContext()
	defer cancel()
	if _, err := m.Db.DeleteFromReplicationDLQTasks(ctx, sqlplugin.ReplicationDLQTasksFilter{
		ShardID:           request.ShardID,
		TaskID:            request.TaskID,
		SourceClusterName: request.SourceClusterName,
	}); err != nil {
		return err
	}
	return nil
}

func (m *sqlExecutionStore) RangeDeleteReplicationTaskFromDLQ(
	request *p.RangeDeleteReplicationTaskFromDLQRequest,
) error {
	ctx, cancel := newExecutionContext()
	defer cancel()
	if _, err := m.Db.RangeDeleteFromReplicationDLQTasks(ctx, sqlplugin.ReplicationDLQTasksRangeFilter{
		ShardID:           request.ShardID,
		SourceClusterName: request.SourceClusterName,
		MinTaskID:         request.ExclusiveBeginTaskID,
		MaxTaskID:         request.InclusiveEndTaskID,
	}); err != nil {
		return err
	}
	return nil
}

func (m *sqlExecutionStore) GetVisibilityTask(
	request *persistence.GetVisibilityTaskRequest,
) (*persistence.GetVisibilityTaskResponse, error) {
	ctx, cancel := newExecutionContext()
	defer cancel()
	rows, err := m.Db.SelectFromVisibilityTasks(ctx, sqlplugin.VisibilityTasksFilter{
		ShardID: request.ShardID,
		TaskID:  request.TaskID,
	})
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, serviceerror.NewNotFound(fmt.Sprintf("GetVisibilityTask operation failed. Task with ID %v not found. Error: %v", request.TaskID, err))
		}
		return nil, serviceerror.NewUnavailable(fmt.Sprintf("GetVisibilityTask operation failed. Failed to get record. TaskId: %v. Error: %v", request.TaskID, err))
	}

	if len(rows) == 0 {
		return nil, serviceerror.NewUnavailable(fmt.Sprintf("GetVisibilityTask operation failed. Failed to get record. TaskId: %v", request.TaskID))
	}

	visibilityRow := rows[0]
	visibilityInfo, err := serialization.VisibilityTaskInfoFromBlob(visibilityRow.Data, visibilityRow.DataEncoding)
	if err != nil {
		return nil, err
	}

	resp := &persistence.GetVisibilityTaskResponse{VisibilityTaskInfo: visibilityInfo}

	return resp, nil
}

func (m *sqlExecutionStore) GetVisibilityTasks(
	request *p.GetVisibilityTasksRequest,
) (*p.GetVisibilityTasksResponse, error) {
	ctx, cancel := newExecutionContext()
	defer cancel()
	rows, err := m.Db.RangeSelectFromVisibilityTasks(ctx, sqlplugin.VisibilityTasksRangeFilter{
		ShardID:   request.ShardID,
		MinTaskID: request.ReadLevel,
		MaxTaskID: request.MaxReadLevel,
	})
	if err != nil {
		if err != sql.ErrNoRows {
			return nil, serviceerror.NewUnavailable(fmt.Sprintf("GetVisibilityTasks operation failed. Select failed. Error: %v", err))
		}
	}
	resp := &p.GetVisibilityTasksResponse{Tasks: make([]*persistencespb.VisibilityTaskInfo, len(rows))}
	for i, row := range rows {
		info, err := serialization.VisibilityTaskInfoFromBlob(row.Data, row.DataEncoding)
		if err != nil {
			return nil, err
		}
		resp.Tasks[i] = info
	}

	return resp, nil
}

func (m *sqlExecutionStore) CompleteVisibilityTask(
	request *p.CompleteVisibilityTaskRequest,
) error {
	ctx, cancel := newExecutionContext()
	defer cancel()
	if _, err := m.Db.DeleteFromVisibilityTasks(ctx, sqlplugin.VisibilityTasksFilter{
		ShardID: request.ShardID,
		TaskID:  request.TaskID,
	}); err != nil {
		return serviceerror.NewUnavailable(fmt.Sprintf("CompleteVisibilityTask operation failed. Error: %v", err))
	}
	return nil
}

func (m *sqlExecutionStore) RangeCompleteVisibilityTask(
	request *p.RangeCompleteVisibilityTaskRequest,
) error {
	ctx, cancel := newExecutionContext()
	defer cancel()
	if _, err := m.Db.RangeDeleteFromVisibilityTasks(ctx, sqlplugin.VisibilityTasksRangeFilter{
		ShardID:   request.ShardID,
		MinTaskID: request.ExclusiveBeginTaskID,
		MaxTaskID: request.InclusiveEndTaskID,
	}); err != nil {
		return serviceerror.NewUnavailable(fmt.Sprintf("RangeCompleteVisibilityTask operation failed. Error: %v", err))
	}
	return nil
}

type timerTaskPageToken struct {
	TaskID    int64
	Timestamp time.Time
}

func (t *timerTaskPageToken) serialize() ([]byte, error) {
	return json.Marshal(t)
}

func (t *timerTaskPageToken) deserialize(payload []byte) error {
	return json.Unmarshal(payload, t)
}
