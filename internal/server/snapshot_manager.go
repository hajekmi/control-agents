package server

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"regexp"
	"sync"
	"time"

	"control-agents/internal/tmux"
)

const (
	defaultSnapshotIdleTTL         = 10 * time.Minute
	defaultSnapshotsPerViewer      = 2
	defaultSnapshotsPerUser        = 8
	defaultSnapshotsPerProcess     = 32
	defaultSnapshotProcessMaxBytes = 128 * 1024 * 1024
	defaultSnapshotProcessMaxNodes = 1_000_000
	defaultHistoryPageMaxLines     = 3000
	defaultHistoryPageMaxBytes     = 2 * 1024 * 1024
	historyPageEnvelopeMaxBytes    = 512
)

var (
	errSnapshotGone       = errors.New("history snapshot is gone")
	errSnapshotNotFound   = errors.New("history snapshot was not found")
	errSnapshotCapacity   = errors.New("history snapshot capacity reached")
	errSnapshotCursor     = errors.New("invalid history snapshot cursor")
	historySnapshotIDExpr = regexp.MustCompile(`^hs_[A-Za-z0-9_-]{16,96}$`)
)

type snapshotManagerConfig struct {
	IdleTTL         time.Duration
	PerViewer       int
	PerUser         int
	PerProcess      int
	ProcessMaxBytes int64
	ProcessMaxNodes int
	PageMaxLines    int
	PageMaxBytes    int
}

type snapshotBinding struct {
	SessionRef   SessionRef
	PaneRef      PaneRef
	Generation   PaneGeneration
	OutputEpoch  int64
	HistoryBytes int64
}

type snapshotCreate struct {
	User         string
	Viewer       ViewerID
	SessionRef   SessionRef
	PaneRef      PaneRef
	Generation   PaneGeneration
	Mode         string
	Capture      tmux.HistoryCapture
	Lines        []historyLine
	NodeEstimate int
}

type historySnapshot struct {
	ID         string
	User       string
	Viewer     ViewerID
	Binding    snapshotBinding
	Mode       string
	Capture    tmux.HistoryCapture
	Lines      []historyLine
	Memory     int64
	Nodes      int
	LastAccess time.Time

	cursors        map[string]int
	cursorByBefore map[int]string
}

type historyPageResponse struct {
	SnapshotID       string        `json:"snapshotId"`
	Mode             string        `json:"mode"`
	Columns          int           `json:"columns"`
	Rows             int           `json:"rows"`
	HistorySize      int           `json:"historySize"`
	HistoryLimit     int           `json:"historyLimit"`
	AlternateScreen  bool          `json:"alternateScreen"`
	OutputEpoch      int64         `json:"outputEpoch"`
	FollowedByOutput bool          `json:"followedByOutput"`
	Lines            []historyLine `json:"lines"`
	Before           string        `json:"before,omitempty"`
	HasMore          bool          `json:"hasMore"`
}

type snapshotManager struct {
	mu         sync.Mutex
	config     snapshotManagerConfig
	now        func() time.Time
	snapshots  map[string]*historySnapshot
	memoryUsed int64
	nodesUsed  int
}

func newSnapshotManager(config snapshotManagerConfig) *snapshotManager {
	if config.IdleTTL <= 0 {
		config.IdleTTL = defaultSnapshotIdleTTL
	}
	if config.PerViewer <= 0 {
		config.PerViewer = defaultSnapshotsPerViewer
	}
	if config.PerUser <= 0 {
		config.PerUser = defaultSnapshotsPerUser
	}
	if config.PerProcess <= 0 {
		config.PerProcess = defaultSnapshotsPerProcess
	}
	if config.ProcessMaxBytes <= 0 {
		config.ProcessMaxBytes = defaultSnapshotProcessMaxBytes
	}
	if config.ProcessMaxNodes <= 0 {
		config.ProcessMaxNodes = defaultSnapshotProcessMaxNodes
	}
	if config.PageMaxLines <= 0 {
		config.PageMaxLines = defaultHistoryPageMaxLines
	}
	if config.PageMaxBytes <= 0 {
		config.PageMaxBytes = defaultHistoryPageMaxBytes
	}
	return &snapshotManager{
		config:    config,
		now:       time.Now,
		snapshots: make(map[string]*historySnapshot),
	}
}

func (m *snapshotManager) Create(request snapshotCreate) (historyPageResponse, error) {
	if request.User == "" || !viewerIDPattern.MatchString(string(request.Viewer)) || request.SessionRef == "" || request.PaneRef == "" {
		return historyPageResponse{}, errSnapshotNotFound
	}
	memory, err := estimateSnapshotMemory(request.Lines, m.config.PageMaxBytes)
	if err != nil {
		return historyPageResponse{}, err
	}
	actualNodes := historyNodeEstimate(request.Lines)
	nodes := request.NodeEstimate
	if nodes == 0 {
		nodes = actualNodes
	}
	if nodes < 0 || nodes != actualNodes {
		return historyPageResponse{}, errHistoryANSIResourceLimit
	}
	now := m.now()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneLocked(now)

	viewerCount := 0
	userCount := 0
	for _, snapshot := range m.snapshots {
		if snapshot.User == request.User {
			userCount++
			if snapshot.Viewer == request.Viewer {
				viewerCount++
			}
		}
	}
	if viewerCount >= m.config.PerViewer || userCount >= m.config.PerUser || len(m.snapshots) >= m.config.PerProcess ||
		memory > m.config.ProcessMaxBytes-m.memoryUsed || nodes > m.config.ProcessMaxNodes-m.nodesUsed {
		return historyPageResponse{}, errSnapshotCapacity
	}

	id := newHistoryOpaqueID("hs")
	storedCapture := request.Capture
	storedCapture.Text = ""
	snapshot := &historySnapshot{
		ID:     id,
		User:   request.User,
		Viewer: request.Viewer,
		Binding: snapshotBinding{
			SessionRef:   request.SessionRef,
			PaneRef:      request.PaneRef,
			Generation:   request.Generation,
			OutputEpoch:  request.Capture.After.OutputEpoch,
			HistoryBytes: request.Capture.After.HistoryBytes,
		},
		Mode:           request.Mode,
		Capture:        storedCapture,
		Lines:          request.Lines,
		Memory:         memory,
		Nodes:          nodes,
		LastAccess:     now,
		cursors:        make(map[string]int),
		cursorByBefore: make(map[int]string),
	}
	m.snapshots[id] = snapshot
	m.memoryUsed += memory
	m.nodesUsed += nodes
	return m.pageLocked(snapshot, "")
}

func (m *snapshotManager) Binding(id, user string, viewer ViewerID) (snapshotBinding, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	snapshot, err := m.authorizedLocked(id, user, viewer, m.now())
	if err != nil {
		return snapshotBinding{}, err
	}
	return snapshot.Binding, nil
}

func (m *snapshotManager) Page(id, cursor, user string, viewer ViewerID) (historyPageResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	snapshot, err := m.authorizedLocked(id, user, viewer, m.now())
	if err != nil {
		return historyPageResponse{}, err
	}
	return m.pageLocked(snapshot, cursor)
}

func (m *snapshotManager) Delete(id, user string, viewer ViewerID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	snapshot, err := m.authorizedLocked(id, user, viewer, m.now())
	if err != nil {
		return err
	}
	m.deleteLocked(snapshot.ID)
	return nil
}

func (m *snapshotManager) DeleteUser(user string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, snapshot := range m.snapshots {
		if snapshot.User == user {
			m.deleteLocked(id)
		}
	}
}

func (m *snapshotManager) DeleteSession(ref SessionRef) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, snapshot := range m.snapshots {
		if snapshot.Binding.SessionRef == ref {
			m.deleteLocked(id)
		}
	}
}

func (m *snapshotManager) authorizedLocked(id, user string, viewer ViewerID, now time.Time) (*historySnapshot, error) {
	if !historySnapshotIDExpr.MatchString(id) {
		return nil, errSnapshotNotFound
	}
	m.pruneLocked(now)
	snapshot := m.snapshots[id]
	if snapshot == nil {
		return nil, errSnapshotGone
	}
	if snapshot.User != user || snapshot.Viewer != viewer {
		return nil, errSnapshotNotFound
	}
	snapshot.LastAccess = now
	return snapshot, nil
}

func (m *snapshotManager) pageLocked(snapshot *historySnapshot, cursor string) (historyPageResponse, error) {
	before := len(snapshot.Lines)
	if cursor != "" {
		var ok bool
		before, ok = snapshot.cursors[cursor]
		if !ok {
			return historyPageResponse{}, errSnapshotCursor
		}
	}
	start := before
	bytes := historyPageEnvelopeMaxBytes
	for start > 0 && before-start < m.config.PageMaxLines {
		encoded, err := json.Marshal(snapshot.Lines[start-1])
		if err != nil {
			return historyPageResponse{}, err
		}
		if before-start > 0 && bytes+len(encoded)+1 > m.config.PageMaxBytes {
			break
		}
		bytes += len(encoded) + 1
		start--
	}
	if start == before && start > 0 {
		return historyPageResponse{}, errHistoryANSIResourceLimit
	}
	nextCursor := ""
	if start > 0 {
		nextCursor = snapshot.cursorByBefore[start]
		if nextCursor == "" {
			nextCursor = newHistoryOpaqueID("hc")
			snapshot.cursorByBefore[start] = nextCursor
			snapshot.cursors[nextCursor] = start
		}
	}
	beforeMeta := snapshot.Capture.Before
	afterMeta := snapshot.Capture.After
	return historyPageResponse{
		SnapshotID:       snapshot.ID,
		Mode:             snapshot.Mode,
		Columns:          beforeMeta.Columns,
		Rows:             beforeMeta.Rows,
		HistorySize:      beforeMeta.HistorySize,
		HistoryLimit:     beforeMeta.HistoryLimit,
		AlternateScreen:  beforeMeta.AlternateScreen,
		OutputEpoch:      afterMeta.OutputEpoch,
		FollowedByOutput: afterMeta.OutputEpoch != beforeMeta.OutputEpoch || afterMeta.HistoryBytes != beforeMeta.HistoryBytes,
		Lines:            cloneHistoryLines(snapshot.Lines[start:before]),
		Before:           nextCursor,
		HasMore:          start > 0,
	}, nil
}

func cloneHistoryLines(lines []historyLine) []historyLine {
	cloned := make([]historyLine, len(lines))
	for lineIndex, line := range lines {
		cloned[lineIndex].BidiWarning = line.BidiWarning
		cloned[lineIndex].Runs = make([]historyRun, len(line.Runs))
		for runIndex, run := range line.Runs {
			cloned[lineIndex].Runs[runIndex].Text = run.Text
			if run.Style != nil {
				style := *run.Style
				cloned[lineIndex].Runs[runIndex].Style = &style
			}
		}
	}
	return cloned
}

func (m *snapshotManager) pruneLocked(now time.Time) {
	deadline := now.Add(-m.config.IdleTTL)
	for id, snapshot := range m.snapshots {
		if snapshot.LastAccess.Before(deadline) {
			m.deleteLocked(id)
		}
	}
}

func (m *snapshotManager) deleteLocked(id string) {
	if snapshot := m.snapshots[id]; snapshot != nil {
		m.memoryUsed -= snapshot.Memory
		m.nodesUsed -= snapshot.Nodes
		delete(m.snapshots, id)
	}
}

func estimateSnapshotMemory(lines []historyLine, pageMaxBytes int) (int64, error) {
	var memory int64
	for _, line := range lines {
		encoded, err := json.Marshal(line)
		if err != nil {
			return 0, err
		}
		if len(encoded)+historyPageEnvelopeMaxBytes > pageMaxBytes {
			return 0, errHistoryANSIResourceLimit
		}
		memory += int64(len(encoded) + 32)
		for _, run := range line.Runs {
			memory += int64(len(run.Text) + 96)
		}
	}
	return memory, nil
}

func newHistoryOpaqueID(prefix string) string {
	random := make([]byte, 24)
	if _, err := rand.Read(random); err != nil {
		panic(err)
	}
	return prefix + "_" + base64.RawURLEncoding.EncodeToString(random)
}
