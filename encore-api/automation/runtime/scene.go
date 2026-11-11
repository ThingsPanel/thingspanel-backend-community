package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"encore.app/automation/domain"
	"encore.app/automation/store"
)

const MaxSceneReferences = 256

// ValidateSceneReferences checks the reachable native graph within one tenant.
// Runtime path checks remain authoritative when edits race this preflight.
func (r *Runtime) ValidateSceneReferences(ctx context.Context, tenantID, id string, def domain.Definition) error {
	visiting := map[string]bool{}
	height := map[string]int{}
	nodes := 0
	var visit func(string, int) (int, error)
	visit = func(current string, depth int) (int, error) {
		if visiting[current] {
			return 0, errors.New("scene_cycle: 场景引用形成循环")
		}
		if depth > MaxCausalHop {
			return 0, errors.New("scene_depth: 场景激活不能超过 8 次")
		}
		if h, ok := height[current]; ok {
			if depth+h > MaxCausalHop {
				return 0, errors.New("scene_depth: 场景激活不能超过 8 次")
			}
			return h, nil
		}
		nodes++
		if nodes > MaxSceneReferences {
			return 0, errors.New("scene_graph_limit: 场景引用超过 256 个")
		}
		body := def
		if current != id {
			rule, err := r.store.GetRule(ctx, tenantID, current)
			if errors.Is(err, store.ErrNotFound) {
				return 0, errors.New("scene_unavailable: 引用场景不存在或不属于当前租户")
			}
			if err != nil {
				return 0, err
			}
			if rule.Kind != domain.KindScene {
				return 0, errors.New("scene_kind: 激活目标必须是场景")
			}
			body = rule.Definition
		}
		visiting[current] = true
		h := 0
		var walk func([]domain.Step) error
		walk = func(steps []domain.Step) error {
			for _, step := range steps {
				if step.Kind == domain.StepActivateScene {
					child, err := visit(step.Value, depth+1)
					if err != nil {
						return err
					}
					if child+1 > h {
						h = child + 1
					}
				}
				if err := walk(step.Then); err != nil {
					return err
				}
				if err := walk(step.Else); err != nil {
					return err
				}
			}
			return nil
		}
		if err := walk(body.Steps); err != nil {
			return 0, err
		}
		delete(visiting, current)
		height[current] = h
		return h, nil
	}
	_, err := visit(id, 0)
	return err
}

func (r *Runtime) dispatchScene(ctx context.Context, item *store.OutboxItem) (bool, string, string, string, error) {
	fail := func(message string) (bool, string, string, string, error) {
		return r.finishDispatch(ctx, item, "failed", "", message, nil)
	}
	var payload struct {
		SceneID string `json:"scene_id"`
	}
	if err := json.Unmarshal(item.Payload, &payload); err != nil || payload.SceneID == "" {
		return fail("scene_payload: 场景引用无效")
	}
	parent, err := r.store.GetRun(ctx, item.TenantID, item.RunID)
	if errors.Is(err, store.ErrNotFound) {
		return fail("scene_parent: 父运行不存在")
	}
	if err != nil {
		return false, "", "", "", err
	}
	if item.IdempotencyKey == "" {
		return fail("scene_identity: 缺少动作身份")
	}
	digest := sha256.Sum256([]byte(item.TenantID + "\x00" + item.IdempotencyKey))
	childID := "scene-" + hex.EncodeToString(digest[:16])
	accept := func(child *store.Run) (bool, string, string, string, error) {
		if child.RuleID != payload.SceneID || child.CauseEventID != item.ID {
			return fail("scene_identity_conflict: 场景动作身份冲突")
		}
		switch child.Status {
		case StatusIgnored, StatusRejected, StatusExpired, StatusCancelled:
			return fail("scene_not_admitted: " + child.ReasonCode + " " + child.Reason)
		default:
			return r.finishDispatch(ctx, item, "accepted", child.ID, "原生场景运行已受理；子运行结果另行记录", nil)
		}
	}
	if prior, err := r.store.GetRun(ctx, item.TenantID, childID); err == nil {
		return accept(prior)
	} else if !errors.Is(err, store.ErrNotFound) {
		return false, "", "", "", err
	}
	scene, err := r.store.GetRule(ctx, item.TenantID, payload.SceneID)
	if errors.Is(err, store.ErrNotFound) {
		return fail("scene_unavailable: 目标场景不存在或不属于当前租户")
	}
	if err != nil {
		return false, "", "", "", err
	}
	if scene.Kind != domain.KindScene {
		return fail("scene_kind: 激活目标必须是场景")
	}
	if !scene.Enabled {
		return fail("scene_disabled: 目标场景已停用")
	}
	var snapshot struct {
		ScenePath []string `json:"scene_path"`
	}
	if err := json.Unmarshal(parent.Snapshot, &snapshot); err != nil {
		return fail("scene_parent: 父运行快照无效")
	}
	path := snapshot.ScenePath
	if len(path) == 0 {
		path = []string{parent.RuleID}
	}
	for _, ancestor := range path {
		if ancestor == scene.ID {
			return fail("scene_cycle: 场景引用形成循环")
		}
	}
	if parent.CausalHop >= MaxCausalHop || len(path) > MaxCausalHop {
		return fail("scene_depth: 场景激活不能超过 8 次")
	}
	root := parent.CausalRoot
	if root == "" {
		root = parent.ID
	}
	subject := ""
	if scene.Definition.Scope == domain.ScopeDevice {
		if len(scene.Definition.Targets.DeviceIDs) != 1 {
			return fail("scene_subject: 按设备隔离的场景必须有唯一目标设备")
		}
		subject = scene.Definition.Targets.DeviceIDs[0]
	}
	c := cause{Root: root, Hop: parent.CausalHop + 1, EventID: item.ID, ScenePath: append(append([]string{}, path...), scene.ID)}
	result := domain.Result{Verdict: domain.VerdictTrue, Reasons: []domain.Reason{{Code: "scene_activation", Message: fmt.Sprintf("由父运行 %s 激活", parent.ID)}}}
	child, err := r.acceptRunWithIdentity(ctx, scene, subject, item.ID, c, result, r.Now(), childID, true)
	if err != nil {
		return false, "", "", "", err
	}
	return accept(child)
}
