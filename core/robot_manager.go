package core

import (
	"context"
	"fmt"
	"gpt-robot/businesshost"
	"gpt-robot/model"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// RobotManager 保存工作台目录注册表，001原地保留，其他实例使用独立数据根目录。
type RobotManager struct {
	mu         sync.Mutex
	root       string
	store      *Store
	entries    []model.RobotEntry
	services   map[string]*Service
	contexts   map[string]context.Context
	cancels    map[string]context.CancelFunc
	done       map[string]chan struct{}
	background context.Context
}
type RobotManagerInterface interface {
	List(context.Context) ([]model.RobotView, error)
	Create(context.Context, *model.RobotManagerInput) (model.RobotEntry, error)
	SoftDelete(context.Context, *model.RobotManagerInput, string) error
	Restore(context.Context, *model.RobotManagerInput) error
	Control(context.Context, *model.RobotManagerInput, string) error
	Resolve(context.Context, string) (*Service, context.Context, context.CancelFunc, error)
	Run(context.Context)
}

var _ RobotManagerInterface = (*RobotManager)(nil)

func NewRobotManager(ctx context.Context, root string, primary *Service) (*RobotManager, error) {
	m := &RobotManager{root: root, store: primary.Store, services: map[string]*Service{"001": primary}, contexts: map[string]context.Context{}, cancels: map[string]context.CancelFunc{}, done: map[string]chan struct{}{}}
	found, err := m.store.Read(ctx, "data/robot-registry.json", &m.entries)
	if err != nil {
		return nil, err
	}
	if !found {
		m.entries = []model.RobotEntry{{ID: "001", Name: "机器人001", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}}
		if err = m.save(ctx); err != nil {
			return nil, err
		}
	}
	seen := map[string]bool{}
	for _, entry := range m.entries {
		if !validRobotID(entry.ID) || seen[entry.ID] {
			return nil, model.Fail("DATA", "机器人注册表编号无效")
		}
		seen[entry.ID] = true
		if entry.ID == "001" {
			if entry.DeletedAt != "" {
				input := model.RobotControlInput{Action: "stop"}
				if _, err = primary.ControlRobot(ctx, &input); err != nil {
					return nil, err
				}
			}
			continue
		}
		if entry.DeletedAt == "" {
			service, err := m.open(ctx, entry.ID)
			if err != nil {
				return nil, err
			}
			m.services[entry.ID] = service
		}
	}
	if !seen["001"] {
		return nil, model.Fail("DATA", "现有001机器人缺失，未覆盖注册表")
	}
	return m, nil
}
func validRobotID(id string) bool {
	if len(id) < 3 || len(id) > 6 {
		return false
	}
	for _, c := range id {
		if c < '0' || c > '9' {
			return false
		}
	}
	n, err := strconv.Atoi(id)
	return err == nil && n > 0
}
func (m *RobotManager) save(ctx context.Context) error {
	return m.store.Write(ctx, "data/robot-registry.json", m.entries)
}
func (m *RobotManager) open(ctx context.Context, id string) (*Service, error) {
	root := filepath.Join(m.root, "data/instances", id)
	s := NewService(root, nil)
	if err := s.Initialize(ctx); err != nil {
		return nil, err
	}
	s.VideoReader = businesshost.NewVideoReader(root, s.modelRouter.Config, m.root)
	return s, nil
}
func (m *RobotManager) List(ctx context.Context) ([]model.RobotView, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	items := []model.RobotView{}
	for _, entry := range m.entries {
		item := model.RobotView{ID: entry.ID, Name: entry.Name, CreatedAt: entry.CreatedAt, DeletedAt: entry.DeletedAt, Status: "stopped"}
		if entry.DeletedAt != "" {
			item.Status = "deleted"
		} else if s := m.services[entry.ID]; s != nil {
			status, err := s.RobotStatus(ctx)
			if err != nil {
				return nil, err
			}
			item.Status = status.Status
		}
		items = append(items, item)
	}
	return items, nil
}
func (m *RobotManager) Create(ctx context.Context, input *model.RobotManagerInput) (model.RobotEntry, error) {
	if input == nil || !model.Text(input.Name, 40) || model.ContainsSensitiveText(input.Name) {
		return model.RobotEntry{}, model.Fail("INPUT", "请输入1–40字的机器人名称")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.entries) >= 100 {
		return model.RobotEntry{}, model.Fail("LIMIT", "当前最多保存100个机器人编号（含已删除）")
	}
	number := 1
	for _, entry := range m.entries {
		n, _ := strconv.Atoi(entry.ID)
		number = max(number, n)
	}
	id := fmt.Sprintf("%03d", number+1)
	if _, statErr := os.Stat(filepath.Join(m.root, "data/instances", id)); statErr == nil {
		return model.RobotEntry{}, model.Fail("CONFLICT", "目标实例目录已存在，未覆盖资料")
	}
	s, err := m.open(ctx, id)
	if err != nil {
		return model.RobotEntry{}, err
	}
	stop := model.RobotControlInput{Action: "stop"}
	if _, err = s.ControlRobot(ctx, &stop); err != nil {
		return model.RobotEntry{}, err
	}
	entry := model.RobotEntry{ID: id, Name: input.Name, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	m.entries = append(m.entries, entry)
	if err = m.save(ctx); err != nil {
		m.entries = m.entries[:len(m.entries)-1]
		s.Store.Database.Close()
		return model.RobotEntry{}, err
	}
	m.services[id] = s
	if m.background != nil {
		m.startService(id, s)
	}
	return entry, nil
}
func (m *RobotManager) SoftDelete(ctx context.Context, input *model.RobotManagerInput, selected string) error {
	if input == nil || !validRobotID(input.ID) || input.ConfirmID != input.ID {
		return model.Fail("CONFIRM", "请完整输入目标机器人编号以确认软删除")
	}
	if input.ID == selected {
		return model.Fail("CONFIRM", "请先切换到其他机器人，再删除当前管理对象")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	selectedActive := false
	for _, entry := range m.entries {
		if entry.ID == selected && entry.DeletedAt == "" {
			selectedActive = true
		}
	}
	if !selectedActive {
		return model.Fail("CONFIRM", "请先选择有效的其他机器人")
	}
	for i, entry := range m.entries {
		if entry.ID != input.ID {
			continue
		}
		if entry.DeletedAt != "" {
			return nil
		}
		if s := m.services[entry.ID]; s != nil {
			stop := model.RobotControlInput{Action: "stop"}
			if _, err := s.ControlRobot(ctx, &stop); err != nil {
				return err
			}
		}
		if cancel := m.cancels[entry.ID]; cancel != nil {
			cancel()
		}
		m.entries[i].DeletedAt = time.Now().UTC().Format(time.RFC3339Nano)
		if err := m.save(ctx); err != nil {
			m.entries[i].DeletedAt = ""
			return err
		}
		return nil
	}
	return model.Fail("NOT_FOUND", "机器人不存在")
}
func (m *RobotManager) Restore(ctx context.Context, input *model.RobotManagerInput) error {
	if input == nil || !validRobotID(input.ID) {
		return model.Fail("INPUT", "机器人编号无效")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, entry := range m.entries {
		if entry.ID != input.ID {
			continue
		}
		if entry.DeletedAt == "" {
			return nil
		}
		s := m.services[entry.ID]
		if done := m.done[entry.ID]; done != nil {
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				return model.Fail("BUSY", "旧任务仍在停止，请稍后恢复")
			}
		}
		if s == nil {
			var err error
			s, err = m.open(ctx, entry.ID)
			if err != nil {
				return err
			}
			m.services[entry.ID] = s
		}
		stop := model.RobotControlInput{Action: "stop"}
		if _, err := s.ControlRobot(ctx, &stop); err != nil {
			return err
		}
		m.entries[i].DeletedAt = ""
		if err := m.save(ctx); err != nil {
			m.entries[i].DeletedAt = entry.DeletedAt
			return err
		}
		if m.background != nil {
			m.startService(entry.ID, s)
		}
		return nil
	}
	return model.Fail("NOT_FOUND", "回收站无此机器人")
}
func (m *RobotManager) Control(ctx context.Context, input *model.RobotManagerInput, action string) error {
	if input == nil || (action != "start" && action != "stop" && action != "pause" && action != "resume") {
		return model.Fail("INPUT", "无效运行控制")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, entry := range m.entries {
		if entry.ID == input.ID {
			if entry.DeletedAt != "" {
				return model.Fail("ROBOT_DELETED", "请先恢复机器人")
			}
			control := model.RobotControlInput{Action: action}
			_, err := m.services[entry.ID].ControlRobot(ctx, &control)
			return err
		}
	}
	return model.Fail("NOT_FOUND", "机器人不存在")
}
func (m *RobotManager) Resolve(ctx context.Context, id string) (*Service, context.Context, context.CancelFunc, error) {
	if id == "" {
		id = "001"
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, entry := range m.entries {
		if entry.ID != id {
			continue
		}
		if entry.DeletedAt != "" {
			return nil, nil, nil, model.Fail("ROBOT_DELETED", "机器人已软删除，请先恢复")
		}
		s := m.services[id]
		if s == nil {
			return nil, nil, nil, model.Fail("NOT_FOUND", "机器人资料尚未就绪")
		}
		request, cancel := context.WithCancel(ctx)
		if life := m.contexts[id]; life != nil {
			stop := context.AfterFunc(life, cancel)
			return s, request, joinRobotCancel(cancel, stop), nil
		}
		return s, request, cancel, nil
	}
	return nil, nil, nil, model.Fail("NOT_FOUND", "机器人不存在")
}
func joinRobotCancel(cancel context.CancelFunc, stop func() bool) context.CancelFunc {
	return func() { stop(); cancel() }
}
func (m *RobotManager) startService(id string, s *Service) {
	life, cancel := context.WithCancel(m.background)
	done := make(chan struct{})
	m.contexts[id] = life
	m.cancels[id] = cancel
	m.done[id] = done
	go runManagedRobot(life, s, done)
}
func runManagedRobot(ctx context.Context, s *Service, done chan struct{}) {
	defer close(done)
	s.RunAutomation(ctx)
}
func (m *RobotManager) Run(ctx context.Context) {
	m.mu.Lock()
	m.background = ctx
	for _, entry := range m.entries {
		if entry.DeletedAt == "" {
			m.startService(entry.ID, m.services[entry.ID])
		}
	}
	m.mu.Unlock()
	<-ctx.Done()
	m.mu.Lock()
	cancels := []context.CancelFunc{}
	done := []chan struct{}{}
	for id, cancel := range m.cancels {
		cancels = append(cancels, cancel)
		done = append(done, m.done[id])
	}
	m.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
	for _, ch := range done {
		<-ch
	}
}
