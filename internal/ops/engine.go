// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

// Package ops implements the durable operations engine (ADR 0008): every change to a cluster is
// planned first, confirmed by a user, then executed step by step with health gates; progress
// is stored in PostgreSQL so operations survive server restarts.
package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/bvaliev/brokerium/internal/cluster"
	"github.com/bvaliev/brokerium/internal/kafka"
	"github.com/bvaliev/brokerium/internal/metrics"
	"github.com/bvaliev/brokerium/internal/store"
)

// Errors returned to the API.
var (
	ErrInvalidState = errors.New("operation is not in a state that allows this action")
	ErrPlanExpired  = errors.New("the plan is older than the allowed age; create a new plan")
	ErrConfirmName  = errors.New("irreversible operation: type the cluster name to confirm")
	ErrNotAllowed   = errors.New("this cluster does not support the operation")
)

// StepFunc executes one step kind. It must be idempotent: after a restart the engine runs the
// current step again.
type StepFunc func(ctx context.Context, x *Exec, args json.RawMessage) (detail any, err error)

// Exec is the execution context passed to steps.
type Exec struct {
	Engine   *Engine
	Op       *store.Operation
	Cluster  *store.Cluster
	Client   *kafka.Client
	progress func(detail any)
}

// Progress publishes intermediate detail of the running step.
func (x *Exec) Progress(detail any) {
	if x.progress != nil {
		x.progress(detail)
	}
}

// Planner gives operation types access to the cluster state at planning time.
type Planner struct {
	Engine  *Engine
	Cluster *store.Cluster
	Client  *kafka.Client
	View    *cluster.View
}

// StepSpec is one planned step.
type StepSpec struct {
	Name      string
	Kind      string
	Args      any
	AlwaysRun bool
}

// Planned is the output of an operation type's planner.
type Planned struct {
	Doc   PlanDoc
	Steps []StepSpec
}

// OpType describes an operation type.
type OpType struct {
	Type         string   `json:"type"`
	Title        string   `json:"title"`
	Description  string   `json:"description"`
	Scope        string   `json:"scope"`
	Requires     []string `json:"requires"`
	Irreversible bool     `json:"irreversible"`
	Plan         func(ctx context.Context, p *Planner, params json.RawMessage) (*Planned, error) `json:"-"`
}

// NodeManager performs node lifecycle actions through agents or providers.
type NodeManager interface {
	StepKinds() map[string]StepFunc
	Types() []OpType
	// StopBrokerStep returns a step that stops a broker node, if the cluster's nodes are managed.
	StopBrokerStep(p *Planner, broker int32) (StepSpec, bool)
	// StopControllerStep returns a step that stops a controller node, if managed.
	StopControllerStep(p *Planner, node int32) (StepSpec, bool)
}

// Engine runs operations.
type Engine struct {
	Store    *store.Store
	Clusters *cluster.Service
	Sampler  *metrics.Sampler
	Log      *slog.Logger
	PlanTTL  time.Duration

	types map[string]OpType
	kinds map[string]StepFunc

	stopStep           func(p *Planner, broker int32) (StepSpec, bool)
	stopControllerStep func(p *Planner, node int32) (StepSpec, bool)

	mu      sync.Mutex
	running map[string]context.CancelFunc
	wake    chan struct{}
}

// New creates an engine with the built-in operation types and step kinds.
func New(st *store.Store, clusters *cluster.Service, sampler *metrics.Sampler, log *slog.Logger) *Engine {
	e := &Engine{Store: st, Clusters: clusters, Sampler: sampler, Log: log, PlanTTL: 15 * time.Minute,
		types: map[string]OpType{}, kinds: map[string]StepFunc{}, running: map[string]context.CancelFunc{},
		wake: make(chan struct{}, 1)}
	for _, t := range builtinTypes() {
		e.types[t.Type] = t
	}
	for k, f := range builtinSteps() {
		e.kinds[k] = f
	}
	return e
}

// Extend registers node lifecycle operations and steps.
func (e *Engine) Extend(m NodeManager) {
	for _, t := range m.Types() {
		e.types[t.Type] = t
	}
	for k, f := range m.StepKinds() {
		e.kinds[k] = f
	}
	e.stopStep = m.StopBrokerStep
	e.stopControllerStep = m.StopControllerStep
}

// Types lists operation types sorted by name.
func (e *Engine) Types() []OpType {
	out := make([]OpType, 0, len(e.types))
	for _, t := range e.types {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Type < out[j].Type })
	return out
}

// Plan computes a plan and stores it as a planned operation.
func (e *Engine) Plan(ctx context.Context, clusterID, typ string, params json.RawMessage, user string) (*store.Operation, error) {
	t, ok := e.types[typ]
	if !ok {
		return nil, fmt.Errorf("unknown operation type %q", typ)
	}
	view, err := e.Clusters.Refresh(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	if view.Snapshot == nil {
		return nil, fmt.Errorf("cluster is unreachable: %s", view.Error)
	}
	if missing := missingCapabilities(view.Capabilities, t.Requires); len(missing) > 0 {
		return nil, fmt.Errorf("%w: requires %v", ErrNotAllowed, missing)
	}
	cl, c, err := e.Clusters.Client(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	if len(params) == 0 {
		params = json.RawMessage("{}")
	}
	planned, err := t.Plan(ctx, &Planner{Engine: e, Cluster: c, Client: cl, View: view}, params)
	if err != nil {
		return nil, err
	}
	if planned.Doc.Warnings == nil {
		planned.Doc.Warnings = []string{}
	}
	planned.Doc.Irreversible = planned.Doc.Irreversible || t.Irreversible
	doc, _ := json.Marshal(planned.Doc)
	op := &store.Operation{ClusterID: clusterID, Type: typ, Status: store.OpPlanned, Summary: planned.Doc.Summary,
		Params: params, Plan: doc, Irreversible: planned.Doc.Irreversible, CreatedBy: user}
	for _, s := range planned.Steps {
		args, _ := json.Marshal(s.Args)
		op.Steps = append(op.Steps, &store.Step{Name: s.Name, Kind: s.Kind, Args: args, AlwaysRun: s.AlwaysRun})
	}
	if err := e.Store.CreateOperation(ctx, op); err != nil {
		return nil, err
	}
	return e.Store.GetOperation(ctx, op.ID)
}

// Confirm starts a planned operation.
func (e *Engine) Confirm(ctx context.Context, id, confirmName string) (*store.Operation, error) {
	op, err := e.Store.GetOperation(ctx, id)
	if err != nil {
		return nil, err
	}
	if op.Status != store.OpPlanned {
		return nil, ErrInvalidState
	}
	if time.Since(op.CreatedAt) > e.PlanTTL {
		return nil, ErrPlanExpired
	}
	if op.Irreversible {
		c, err := e.Store.GetCluster(ctx, op.ClusterID)
		if err != nil {
			return nil, err
		}
		if confirmName != c.Name {
			return nil, ErrConfirmName
		}
	}
	if err := e.Store.SetOperationStatus(ctx, id, store.OpPending, ""); err != nil {
		return nil, err
	}
	e.poke()
	return e.Store.GetOperation(ctx, id)
}

// Pause asks a running operation to stop before its next step.
func (e *Engine) Pause(ctx context.Context, id string) (*store.Operation, error) {
	op, err := e.Store.GetOperation(ctx, id)
	if err != nil {
		return nil, err
	}
	switch op.Status {
	case store.OpRunning:
		err = e.Store.SetRequestedAction(ctx, id, "pause")
	case store.OpPending:
		err = e.Store.SetOperationStatus(ctx, id, store.OpPaused, "")
	default:
		return nil, ErrInvalidState
	}
	if err != nil {
		return nil, err
	}
	return e.Store.GetOperation(ctx, id)
}

// Resume continues a paused operation from its current step.
func (e *Engine) Resume(ctx context.Context, id string) (*store.Operation, error) {
	op, err := e.Store.GetOperation(ctx, id)
	if err != nil {
		return nil, err
	}
	if op.Status != store.OpPaused {
		return nil, ErrInvalidState
	}
	if err := e.Store.SetRequestedAction(ctx, id, ""); err != nil {
		return nil, err
	}
	if err := e.Store.SetOperationStatus(ctx, id, store.OpPending, ""); err != nil {
		return nil, err
	}
	e.poke()
	return e.Store.GetOperation(ctx, id)
}

// Cancel stops an operation; cleanup steps (always-run) still execute.
func (e *Engine) Cancel(ctx context.Context, id string) (*store.Operation, error) {
	op, err := e.Store.GetOperation(ctx, id)
	if err != nil {
		return nil, err
	}
	switch op.Status {
	case store.OpPlanned:
		err = e.Store.SetOperationStatus(ctx, id, store.OpCancelled, "discarded")
	case store.OpPending, store.OpPaused:
		if err = e.Store.SetRequestedAction(ctx, id, "cancel"); err == nil {
			err = e.Store.SetOperationStatus(ctx, id, store.OpPending, "")
		}
	case store.OpRunning:
		if err = e.Store.SetRequestedAction(ctx, id, "cancel"); err == nil {
			e.mu.Lock()
			if cancel, ok := e.running[id]; ok {
				cancel()
			}
			e.mu.Unlock()
		}
	default:
		return nil, ErrInvalidState
	}
	if err != nil {
		return nil, err
	}
	e.poke()
	return e.Store.GetOperation(ctx, id)
}

func (e *Engine) poke() {
	select {
	case e.wake <- struct{}{}:
	default:
	}
}

// Run executes pending operations until ctx ends. Operations left running by a previous process
// are resumed from their current step.
func (e *Engine) Run(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		e.tick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-e.wake:
		}
	}
}

func (e *Engine) tick(ctx context.Context) {
	ops, err := e.Store.OperationsInStatus(ctx, store.OpPending, store.OpRunning)
	if err != nil {
		if ctx.Err() == nil {
			e.Log.Warn("listing operations", "error", err)
		}
		return
	}
	for _, op := range ops {
		e.mu.Lock()
		_, busy := e.running[op.ID]
		if !busy {
			e.running[op.ID] = func() {}
		}
		e.mu.Unlock()
		if busy {
			if op.RequestedAction == "cancel" {
				e.mu.Lock()
				e.running[op.ID]()
				e.mu.Unlock()
			}
			continue
		}
		go e.execute(ctx, op.ID)
	}
}

func (e *Engine) execute(parent context.Context, id string) {
	defer func() {
		e.mu.Lock()
		delete(e.running, id)
		e.mu.Unlock()
	}()
	ctx := context.WithoutCancel(parent)
	op, err := e.Store.GetOperation(ctx, id)
	if err != nil {
		return
	}
	log := e.Log.With("operation", op.ID, "type", op.Type)
	cl, c, err := e.Clusters.Client(ctx, op.ClusterID)
	if err != nil {
		_ = e.Store.SetOperationStatus(ctx, id, store.OpFailed, "cluster client: "+err.Error())
		return
	}
	x := &Exec{Engine: e, Op: op, Cluster: c, Client: cl}
	if op.RequestedAction == "cancel" {
		e.finish(ctx, x, op.CurrentStep, store.OpCancelled, "cancelled by user")
		return
	}
	if err := e.Store.SetOperationStatus(ctx, id, store.OpRunning, ""); err != nil {
		return
	}
	log.Info("operation started", "step", op.CurrentStep)
	for i := op.CurrentStep; i < len(op.Steps); i++ {
		st := op.Steps[i]
		if st.Status == store.StepSucceeded || st.Status == store.StepSkipped {
			continue
		}
		cur, err := e.Store.GetOperation(ctx, id)
		if err == nil {
			switch cur.RequestedAction {
			case "pause":
				_ = e.Store.SetRequestedAction(ctx, id, "")
				_ = e.Store.SetOperationStatus(ctx, id, store.OpPaused, "")
				log.Info("operation paused", "step", i)
				return
			case "cancel":
				e.finish(ctx, x, i, store.OpCancelled, "cancelled by user")
				return
			}
		}
		_ = e.Store.SetCurrentStep(ctx, id, i)
		ok := e.runStep(parent, x, st)
		if parent.Err() != nil {
			// The server is shutting down; the step runs again after restart.
			return
		}
		if !ok {
			cur, _ := e.Store.GetOperation(ctx, id)
			if cur != nil && cur.RequestedAction == "cancel" {
				e.finish(ctx, x, i+1, store.OpCancelled, "cancelled by user during step "+st.Name)
				return
			}
			e.finish(ctx, x, i+1, store.OpFailed, fmt.Sprintf("step %q failed", st.Name))
			return
		}
	}
	_ = e.Store.SetOperationStatus(ctx, id, store.OpSucceeded, "")
	e.audit(ctx, op, "ok")
	log.Info("operation succeeded")
	go e.Clusters.Refresh(context.Background(), op.ClusterID) //nolint:errcheck
}

// runStep executes one step and records its outcome; it returns false on failure.
func (e *Engine) runStep(parent context.Context, x *Exec, st *store.Step) bool {
	ctx := context.WithoutCancel(parent)
	fn, ok := e.kinds[st.Kind]
	if !ok {
		_ = e.Store.UpdateStep(ctx, x.Op.ID, st.Index, store.StepFailed, nil, "unknown step kind "+st.Kind)
		return false
	}
	_ = e.Store.UpdateStep(ctx, x.Op.ID, st.Index, store.StepRunning, st.Detail, "")
	stepCtx, cancel := context.WithCancel(parent)
	defer cancel()
	e.mu.Lock()
	e.running[x.Op.ID] = cancel
	e.mu.Unlock()
	var lastProgress time.Time
	x.progress = func(detail any) {
		if time.Since(lastProgress) < time.Second {
			return
		}
		lastProgress = time.Now()
		raw, _ := json.Marshal(detail)
		_ = e.Store.UpdateStep(ctx, x.Op.ID, st.Index, store.StepRunning, raw, "")
	}
	detail, err := safeRun(stepCtx, fn, x, st.Args)
	raw, _ := json.Marshal(detail)
	if err != nil {
		if parent.Err() != nil {
			return false
		}
		e.Log.Warn("step failed", "operation", x.Op.ID, "step", st.Name, "error", err)
		_ = e.Store.UpdateStep(ctx, x.Op.ID, st.Index, store.StepFailed, raw, err.Error())
		return false
	}
	_ = e.Store.UpdateStep(ctx, x.Op.ID, st.Index, store.StepSucceeded, raw, "")
	return true
}

func safeRun(ctx context.Context, fn StepFunc, x *Exec, args json.RawMessage) (detail any, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("step panicked: %v", r)
		}
	}()
	return fn(ctx, x, args)
}

// finish runs the remaining always-run steps from index `from`, skips the others and sets the
// final status.
func (e *Engine) finish(ctx context.Context, x *Exec, from int, status, msg string) {
	for i := from; i < len(x.Op.Steps); i++ {
		st := x.Op.Steps[i]
		if st.Status == store.StepSucceeded {
			continue
		}
		if st.AlwaysRun {
			e.runStep(context.Background(), x, st)
			continue
		}
		_ = e.Store.UpdateStep(ctx, x.Op.ID, st.Index, store.StepSkipped, nil, "")
	}
	_ = e.Store.SetRequestedAction(ctx, x.Op.ID, "")
	_ = e.Store.SetOperationStatus(ctx, x.Op.ID, status, msg)
	e.audit(ctx, x.Op, status+": "+msg)
	go e.Clusters.Refresh(context.Background(), x.Op.ClusterID) //nolint:errcheck
}

func (e *Engine) audit(ctx context.Context, op *store.Operation, result string) {
	detail, _ := json.Marshal(map[string]any{"operation": op.ID, "summary": op.Summary})
	_ = e.Store.AddAudit(ctx, store.AuditEntry{Actor: op.CreatedBy, Action: "operation." + op.Type,
		ClusterID: op.ClusterID, Target: op.ID, Result: result, Detail: detail})
}

func missingCapabilities(c cluster.Capabilities, required []string) []string {
	raw, _ := json.Marshal(c)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	var missing []string
	for _, r := range required {
		if v, ok := m[r].(bool); !ok || !v {
			missing = append(missing, r)
		}
	}
	return missing
}
