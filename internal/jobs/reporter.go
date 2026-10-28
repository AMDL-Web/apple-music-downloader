package jobs

import (
	"context"
	"encoding/json"
	"time"

	"amdl/internal/domain"
	"amdl/internal/storage"
)

func (m *Manager) SetJob(ctx context.Context, job *domain.Job) error {
	job.UpdatedAt = time.Now().UTC()
	return m.store.UpdateJob(ctx, *job)
}

func (m *Manager) SetJobMotionArtwork(ctx context.Context, jobID string, art domain.MotionArtwork) (bool, error) {
	return m.store.SetJobMotionArtwork(ctx, jobID, art)
}

func (m *Manager) AddItem(ctx context.Context, item *domain.JobItem) error {
	now := time.Now().UTC()
	item.CreatedAt = now
	item.UpdatedAt = now
	if item.ID == "" {
		item.ID = storage.NewID("item")
	}
	return m.store.CreateItem(ctx, *item)
}

func (m *Manager) UpdateItem(ctx context.Context, item *domain.JobItem) error {
	item.UpdatedAt = time.Now().UTC()
	return m.store.UpdateItem(ctx, *item)
}

func (m *Manager) RemoveItem(ctx context.Context, itemID string) error {
	return m.store.DeleteItem(ctx, itemID)
}

func (m *Manager) ListItems(ctx context.Context, jobID string) ([]domain.JobItem, error) {
	return m.store.ListItems(ctx, jobID)
}

func (m *Manager) Event(ctx context.Context, ev domain.Event) error {
	if ev.Payload == "" && ev.Message != "" {
		raw, _ := json.Marshal(map[string]string{"message": ev.Message})
		ev.Payload = string(raw)
	}
	stored, err := m.store.AddEvent(ctx, ev)
	if err != nil {
		return err
	}
	m.hub.Publish(stored)
	return nil
}
